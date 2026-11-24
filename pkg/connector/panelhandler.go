// mautrix-telegram - A Matrix-Telegram puppeting bridge.
// Copyright (C) 2026 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/connector/panel"
	"go.mau.fi/mautrix-telegram/pkg/connector/store"
)

// matrixClient returns the raw Matrix client of the appservice bot. The bot
// intent is the only intent we need: it has PL 9001 in the management room, so
// it can redact the user's reactions and read the panel relations.
func (tc *TelegramConnector) matrixClient() *mautrix.Client {
	intent, ok := tc.Bridge.Bot.(*matrix.ASIntent)
	if !ok || intent == nil || intent.Connector == nil || intent.Connector.AS == nil {
		return nil
	}
	return intent.Connector.AS.BotClient()
}

func (tc *TelegramConnector) redactEvent(ctx context.Context, roomID id.RoomID, eventID id.EventID) error {
	client := tc.matrixClient()
	if client == nil {
		return errors.New("no Matrix client available")
	}
	_, err := client.RedactEvent(ctx, roomID, eventID)
	return err
}

func (tc *TelegramConnector) panelLang() string {
	if tc.isRussian() {
		return "ru"
	}
	return "en"
}

// panelRows loads the rows of the current panel page.
func (tc *TelegramClient) panelRows(ctx context.Context, state *store.PanelState) ([]panel.Row, int, error) {
	order := store.ParseCatalogSort(state.Sort)
	offset := state.Page * panel.PageSize
	if state.FilterQuery != "" {
		rows, total, err := tc.main.Store.ChatCatalog.Search(
			ctx, tc.telegramUserID, state.FilterQuery, order, panel.PageSize, offset,
		)
		if err != nil {
			return nil, 0, err
		}
		out := make([]panel.Row, len(rows))
		for i, row := range rows {
			out[i] = tc.panelRowFromCatalog(ctx, row)
		}
		return out, total, nil
	}
	rows, total, err := tc.main.Store.ChatRejection.List(
		ctx, tc.telegramUserID, "", order, panel.PageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	out := make([]panel.Row, len(rows))
	for i, row := range rows {
		out[i] = panel.Row{
			PortalID:     row.ID,
			Title:        row.Title,
			Username:     row.Username,
			Participants: row.Participants,
			Reason:       row.Reason,
			LastSeenTS:   row.LastSeenTS,
			HasMXID:      row.HasMXID,
			Rejected:     true,
			InArchive:    row.InArchive,
			Kind:         row.Kind,
			RoomID:       string(tc.portalMXID(ctx, row.ID)),
		}
		if row.Title == "" {
			// The rejection was recorded before the catalog knew this chat.
			tc.fetchCatalogSnapshotAsync(ctx, networkid.PortalID(row.ID))
		}
	}
	return out, total, nil
}

func (tc *TelegramClient) panelRowFromCatalog(ctx context.Context, row *store.ChatCatalogRow) panel.Row {
	out := panel.Row{
		PortalID:     row.PortalID,
		Title:        row.Title,
		Username:     row.Username,
		Participants: row.Participants,
		InArchive:    row.InArchive,
		Kind:         row.Kind,
		RoomID:       string(tc.portalMXID(ctx, row.PortalID)),
		HasMXID:      false,
	}
	if rejection := tc.main.Store.ChatRejection.GetOne(ctx, tc.telegramUserID, row.PortalID); rejection != nil {
		out.Rejected = true
		out.Reason = rejection.Reason
		out.LastSeenTS = rejection.LastSeenTS
	}
	out.HasMXID = out.RoomID != ""
	// Lazy metadata loading: only for rows the user actually sees.
	if row.Title == "" || row.MetaFetchedTS == 0 {
		tc.fetchCatalogSnapshotAsync(ctx, networkid.PortalID(row.PortalID))
	} else if row.AboutFetchedTS == 0 {
		tc.fetchCatalogAboutAsync(ctx, networkid.PortalID(row.PortalID))
	}
	return out
}

func (tc *TelegramClient) portalMXID(ctx context.Context, portalID string) id.RoomID {
	peerType, chatID, topicID, err := ids.ParsePortalID(networkid.PortalID(portalID))
	if err != nil {
		return ""
	}
	portal, err := tc.main.Bridge.GetExistingPortalByKey(ctx, tc.makePortalKeyFromID(peerType, chatID, topicID))
	if err != nil || portal == nil {
		return ""
	}
	return portal.MXID
}

func (tc *TelegramClient) renderPanel(ctx context.Context, user *bridgev2.User, state *store.PanelState, status string) (string, int, bool, error) {
	rows, total, err := tc.panelRows(ctx, state)
	if err != nil {
		return "", 0, false, err
	}
	needFill := false
	for _, row := range rows {
		if row.Title == "" {
			needFill = true
			break
		}
	}
	state.Page = panel.ClampPage(state.Page, total)
	page := panel.Page{
		UserLabel: fmt.Sprintf("%s (%d)", user.MXID, tc.telegramUserID),
		Query:     state.FilterQuery,
		Sort:      store.ParseCatalogSort(state.Sort),
		PageIndex: state.Page,
		TotalRows: total,
		Offset:    state.Page * panel.PageSize,
		Rows:      rows,
		Status:    status,
		Lang:      tc.main.panelLang(),
	}
	return panel.Render(page), len(rows), needFill, nil
}

// drawPanel creates or edits the single live panel message and re-seeds the
// reaction buttons.
func (tc *TelegramClient) drawPanel(ctx context.Context, user *bridgev2.User, state *store.PanelState, status string) error {
	md, rowCount, needFill, err := tc.renderPanel(ctx, user, state, status)
	if err != nil {
		return err
	}
	content := format.RenderMarkdown(md, true, true)
	content.MsgType = event.MsgNotice
	if state.EventID == "" {
		resp, err := tc.main.Bridge.Bot.SendMessage(
			ctx, state.RoomID, event.EventMessage, &event.Content{Parsed: &content}, nil,
		)
		if err != nil {
			return fmt.Errorf("failed to send panel message: %w", err)
		}
		state.EventID = resp.EventID
	} else {
		content.SetEdit(state.EventID)
		_, err = tc.main.Bridge.Bot.SendMessage(
			ctx, state.RoomID, event.EventMessage, &event.Content{Parsed: &content}, nil,
		)
		if err != nil {
			return fmt.Errorf("failed to edit panel message: %w", err)
		}
	}
	if err = tc.main.Store.PanelState.Save(ctx, state); err != nil {
		return fmt.Errorf("failed to save panel state: %w", err)
	}
	tc.syncPanelReactions(ctx, state.RoomID, state.EventID, rowCount)
	if needFill {
		// Titles are loaded lazily, one API call per missing row. Redraw once when
		// they have landed so the panel fills in without a manual refresh.
		tc.main.schedulePanelTitleRefresh(user.MXID, state.EventID)
	}
	return nil
}

// syncPanelReactions makes the bot's reaction set match the rendered page.
func (tc *TelegramClient) syncPanelReactions(ctx context.Context, roomID id.RoomID, panelEventID id.EventID, rowCount int) {
	client := tc.main.matrixClient()
	if client == nil {
		return
	}
	botMXID := tc.main.Bridge.Bot.GetMXID()
	desired := panel.DesiredKeys(rowCount)
	desiredNorm := make([]string, len(desired))
	for i, key := range desired {
		desiredNorm[i] = panel.NormalizeKey(key)
	}
	existing := make(map[string]bool, len(desired))
	relations, err := client.GetRelations(ctx, roomID, panelEventID, &mautrix.ReqGetRelations{
		RelationType: event.RelAnnotation,
		EventType:    event.EventReaction,
		Limit:        100,
	})
	if err != nil {
		zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to read panel reactions")
	} else {
		for _, relEvt := range relations.Chunk {
			reaction := relEvt.Content.AsReaction()
			if reaction == nil || relEvt.Sender != botMXID {
				continue
			}
			key := panel.NormalizeKey(reaction.RelatesTo.Key)
			if slices.Contains(desiredNorm, key) {
				existing[key] = true
			} else if err := tc.main.redactEvent(ctx, roomID, relEvt.ID); err != nil {
				zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to redact stale panel reaction")
			}
		}
	}
	for i, key := range desired {
		if existing[desiredNorm[i]] {
			continue
		}
		_, err = tc.main.Bridge.Bot.SendMessage(ctx, roomID, event.EventReaction, &event.Content{
			Parsed: &event.ReactionEventContent{
				RelatesTo: event.RelatesTo{
					Type:    event.RelAnnotation,
					EventID: panelEventID,
					Key:     key,
				},
			},
		}, nil)
		if err != nil {
			zerolog.Ctx(ctx).Debug().Err(err).Str("key", key).Msg("Failed to seed panel reaction")
			return
		}
	}
}

// PanelFor renders the panel for a user, creating the message if needed.
func (tc *TelegramClient) PanelFor(ctx context.Context, user *bridgev2.User, roomID id.RoomID, status string) error {
	state, err := tc.main.Store.PanelState.GetOrCreate(ctx, user.MXID, roomID)
	if err != nil {
		return err
	}
	return tc.drawPanel(ctx, user, state, status)
}

type panelReactionRequest struct {
	RoomID    id.RoomID
	EventID   id.EventID
	Sender    id.UserID
	Key       string
	TargetID  id.EventID
	Timestamp time.Time
}

// HandlePanelReaction is the PrependHandler for m.reaction events. It returns
// immediately and does all work in the background, because bridgev2 runs
// prepend handlers in the transactional (Sync) event loop.
func (tc *TelegramConnector) HandlePanelReaction(ctx context.Context, evt *event.Event) {
	reaction := evt.Content.AsReaction()
	if reaction == nil || reaction.RelatesTo.EventID == "" {
		return
	}
	if botMXID := tc.Bridge.Bot.GetMXID(); evt.Sender == botMXID {
		// Our own button reactions and their redactions.
		return
	}
	req := panelReactionRequest{
		RoomID:   evt.RoomID,
		EventID:  evt.ID,
		Sender:   evt.Sender,
		Key:      reaction.RelatesTo.Key,
		TargetID: reaction.RelatesTo.EventID,
	}
	go tc.processPanelReaction(context.WithoutCancel(ctx), req)
}

func (tc *TelegramConnector) processPanelReaction(ctx context.Context, req panelReactionRequest) {
	// The panel state row is the cheapest filter: only a user who opened the panel
	// has one, so reactions from everybody else never touch the user store.
	state, err := tc.Store.PanelState.Get(ctx, req.Sender)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to load panel state for reaction")
		}
		return
	}
	if state.RoomID != req.RoomID {
		return
	}
	user, err := tc.Bridge.GetUserByMXID(ctx, req.Sender)
	if err != nil || user == nil || user.ManagementRoom != req.RoomID {
		return
	}
	login := user.GetDefaultLogin()
	if login == nil {
		return
	}
	client, ok := login.Client.(*TelegramClient)
	if !ok {
		return
	}
	if req.TargetID != state.EventID {
		// Reaction on an older panel: point the user to the live one.
		client.sendEphemeralNotice(ctx, req.RoomID, req.Sender)
		return
	}
	action, ok := panel.ParseKey(req.Key)
	if !ok {
		// Unknown emoji: drop it from the panel and explain nothing, the
		// buttons are documented in the panel footer.
		_ = tc.redactEvent(ctx, req.RoomID, req.EventID)
		return
	}
	if err = tc.redactEvent(ctx, req.RoomID, req.EventID); err != nil {
		zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to redact panel reaction")
	}
	client.applyPanelAction(ctx, user, state, action)
}

// sendEphemeralNotice posts a short-lived notice (deleted after 15s), so old
// panels cannot generate permanent noise in the management room.
func (tc *TelegramClient) sendEphemeralNotice(ctx context.Context, roomID id.RoomID, userID id.UserID) {
	text := tc.main.trf(
		"This panel is outdated, the current one is the last message from the bridge.",
		"Эта панель устарела, актуальная — последнее сообщение от моста.",
	)
	resp, err := tc.main.Bridge.Bot.SendMessage(ctx, roomID, event.EventMessage, &event.Content{
		Parsed: &event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    text,
			Mentions: &event.Mentions{
				UserIDs: []id.UserID{userID},
			},
		},
	}, nil)
	if err != nil {
		zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to send stale panel notice")
		return
	}
	client := tc.main.matrixClient()
	if client == nil {
		return
	}
	time.AfterFunc(15*time.Second, func() {
		if err := tc.main.redactEvent(context.WithoutCancel(ctx), roomID, resp.EventID); err != nil {
			zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to redact stale panel notice")
		}
	})
}

func (tc *TelegramClient) applyPanelAction(
	ctx context.Context, user *bridgev2.User, state *store.PanelState, action panel.Action,
) {
	var status string
	switch action.Kind {
	case panel.ActionPrev:
		if state.Page > 0 {
			state.Page--
		}
	case panel.ActionNext:
		state.Page++
	case panel.ActionSort:
		state.Sort = string(store.ParseCatalogSort(state.Sort).Next())
		state.Page = 0
	case panel.ActionRefresh:
		state.Page = panel.ClampPage(state.Page, tc.panelTotal(ctx, state))
	case panel.ActionSearch:
		if state.FilterQuery != "" {
			state.FilterQuery = ""
			state.Page = 0
			status = tc.main.tr("Filter cleared: showing declined chats", "Фильтр сброшен: показаны отклонённые чаты")
		} else {
			status = tc.main.trf("Search: send `!s <query>`", "Поиск: отправь `!s <запрос>`")
		}
	case panel.ActionRow:
		status = tc.applyRowAction(ctx, user, state, action.Row)
	}
	if err := tc.drawPanel(ctx, user, state, status); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to redraw declined panel")
	}
}

func (tc *TelegramClient) panelTotal(ctx context.Context, state *store.PanelState) int {
	_, total, err := tc.panelRows(ctx, state)
	if err != nil {
		return 0
	}
	return total
}

// applyRowAction is the toggle: un-reject / archive / reject. It returns the
// status line shown on top of the redrawn panel.
func (tc *TelegramClient) applyRowAction(
	ctx context.Context, user *bridgev2.User, state *store.PanelState, rowIndex int,
) string {
	rows, _, err := tc.panelRows(ctx, state)
	if err != nil {
		return tc.main.tr("Failed to load the chat list", "Не удалось загрузить список чатов")
	}
	if rowIndex < 0 || rowIndex >= len(rows) {
		return tc.main.tr("No such row on this page", "На этой странице нет такой строки")
	}
	row := rows[rowIndex]
	portalID := networkid.PortalID(row.PortalID)
	title := row.Title
	if title == "" {
		title = row.PortalID
	}
	rowNumber := state.Page*panel.PageSize + rowIndex + 1
	zerolog.Ctx(ctx).Info().
		Int("page", state.Page).
		Int("row_index", rowIndex).
		Int("row_number", rowNumber).
		Str("portal_id", string(portalID)).
		Str("title", title).
		Bool("rejected", row.Rejected).
		Msg("Panel row action")
	if row.Rejected {
		if row.RoomID != "" || row.HasMXID {
			doublePuppet := user.DoublePuppet(ctx)
			if doublePuppet == nil {
				return tc.main.trf(
					"Double puppeting is required to archive «%s»", "Для архивации «%s» нужен double puppeting",
				)
			}
			if row.RoomID == "" {
				return tc.main.trf(
					"«%s» has a room, but its ID is unknown", "У «%s» есть комната, но её ID не найден",
				)
			}
			if err = doublePuppet.TagRoom(ctx, id.RoomID(row.RoomID), event.RoomTagLowPriority, true); err != nil {
				zerolog.Ctx(ctx).Err(err).Str("room_id", string(row.RoomID)).Msg("Failed to archive room")
				return tc.main.trf("Failed to archive «%s»: %v", "Не удалось архивировать «%s»: %v", title, err)
			}
			return tc.main.trf("🗄 «%s» (%d) moved to the Matrix archive", "🗄 «%s» (строка %d) убран в архив Matrix", title, rowNumber)
		}
		if err = tc.clearPeerRejection(ctx, portalID); err != nil {
			zerolog.Ctx(ctx).Err(err).Str("portal_id", string(portalID)).Msg("Failed to clear rejection")
			return tc.main.trf("Failed to un-decline «%s»: %v", "Не удалось снять отклонение «%s»: %v", title, err)
		}
		return tc.main.trf(
			"✅ «%s» (%d) is unblocked, the room is created on the next message",
			"✅ «%s» (строка %d) разблокирован — комната появится при следующем сообщении", title, rowNumber,
		)
	}
	reason := tc.main.tr("declined manually", "отклонён вручную")
	if err = tc.rejectPeerManually(ctx, portalID, reason, row.HasMXID); err != nil {
		zerolog.Ctx(ctx).Err(err).Str("portal_id", string(portalID)).Msg("Failed to decline chat")
		return tc.main.trf("Failed to decline «%s»: %v", "Не удалось отклонить «%s»: %v", title, err)
	}
	return tc.main.trf(
		"⛔ «%s» (%d) is declined, new messages will not reach Matrix",
		"⛔ «%s» (строка %d) отклонён — новые сообщения в Matrix не попадут", title, rowNumber,
	)
}

// selectAbsoluteRow handles the digit shortcut: the number is the position in
// the whole current list, not only on the visible page.
func (tc *TelegramClient) selectAbsoluteRow(ctx context.Context, user *bridgev2.User, number int) {
	state, err := tc.main.Store.PanelState.GetOrCreate(ctx, user.MXID, user.ManagementRoom)
	if err != nil {
		return
	}
	if number < 1 {
		return
	}
	index := number - 1
	state.Page = index / panel.PageSize
	status := tc.applyRowAction(ctx, user, state, index%panel.PageSize)
	if err = tc.drawPanel(ctx, user, state, status); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to redraw declined panel after digit selection")
	}
}

// panelTitleRefreshes deduplicates the automatic redraw per panel event.
var panelTitleRefreshes sync.Map

// schedulePanelTitleRefresh redraws the panel once, shortly after a render that
// had rows without titles, so lazily fetched names show up without a manual
// refresh. It is keyed by the panel event, so every new panel gets one redraw.
func (tc *TelegramConnector) schedulePanelTitleRefresh(userMXID id.UserID, eventID id.EventID) {
	if _, loaded := panelTitleRefreshes.LoadOrStore(eventID, struct{}{}); loaded {
		return
	}
	time.AfterFunc(7*time.Second, func() {
		panelTitleRefreshes.Delete(eventID)
		tc.redrawPanelIfCurrent(context.Background(), userMXID, eventID)
	})
}

func (tc *TelegramConnector) redrawPanelIfCurrent(ctx context.Context, userMXID id.UserID, eventID id.EventID) {
	state, err := tc.Store.PanelState.Get(ctx, userMXID)
	if err != nil || state.EventID != eventID {
		return
	}
	user, err := tc.Bridge.GetUserByMXID(ctx, userMXID)
	if err != nil || user == nil {
		return
	}
	login := user.GetDefaultLogin()
	if login == nil {
		return
	}
	client, ok := login.Client.(*TelegramClient)
	if !ok || client == nil {
		return
	}
	if err = client.drawPanel(ctx, user, state, ""); err != nil {
		zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to refresh the declined panel after loading titles")
	}
}

// parsePanelNumber extracts the row number from a bare digit command.
func parsePanelNumber(command string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(command))
	if err != nil || n < 1 || n > 99 {
		return 0, false
	}
	return n, true
}
