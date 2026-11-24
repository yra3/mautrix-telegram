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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"

	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/connector/store"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

func (tc *TelegramConnector) declinedCommandHandlers() []commands.CommandHandler {
	handlers := []commands.CommandHandler{
		&commands.FullHandler{
			Func: fnDeclined,
			Name: "declined",
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionChats,
				Description: tc.tr("List, decline or restore rejected Telegram chats", "Список отклонённых чатов Telegram, отклонение и восстановление"),
				Args:        tc.tr("[list [keyword] [-n N] | add <id|keyword> | remove <id|keyword> | purge | archive <id> | leave-tg <id> | panel]", "[list [ключевое слово] [-n N] | add <id|слово> | remove <id|слово> | purge | archive <id> | leave-tg <id> | panel]"),
			},
			RequiresLogin: true,
		},
		&commands.FullHandler{
			Func: fnPanelSearch,
			Name: "/s",
			Aliases: []string{
				"!s", "s",
			},
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionChats,
				Description: tc.tr("Search chats and show the result in the declined-chats panel", "Искать чаты и показать результат в панели отклонённых"),
				Args:        tc.tr("[--refresh] <query>", "[--refresh] <запрос>"),
			},
			RequiresLogin: true,
		},
	}
	digitHandler := &commands.FullHandler{
		Func:          fnPanelDigit,
		Name:          "1",
		Aliases:       panelDigitAliases(),
		RequiresLogin: true,
		Help: commands.HelpMeta{
			Section:     commands.HelpSectionMisc,
			Description: tc.tr("Act on a row of the declined-chats panel (1–99)", "Действие со строкой панели отклонённых чатов (1–99)"),
			Args:        "<1–99>",
		},
	}
	return append(handlers, digitHandler)
}

func panelDigitAliases() []string {
	aliases := make([]string, 0, 98)
	for i := 2; i <= 99; i++ {
		aliases = append(aliases, strconv.Itoa(i))
	}
	return aliases
}

func trfEvent(ce *commands.Event, en, ru string, args ...any) string {
	return fmt.Sprintf(trEvent(ce, en, ru), args...)
}

func loginClientForEvent(ce *commands.Event) (*TelegramClient, *bridgev2.UserLogin) {
	login := ce.User.GetDefaultLogin()
	if login == nil {
		return nil, nil
	}
	client, ok := login.Client.(*TelegramClient)
	if !ok {
		return nil, nil
	}
	return client, login
}

func fnDeclined(ce *commands.Event) {
	client, _ := loginClientForEvent(ce)
	if client == nil {
		ce.Reply(trEvent(ce, "You are not logged in to Telegram", "Вы не авторизованы в Telegram"))
		return
	}
	args := ce.Args
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "list":
		ce.Reply(declinedListText(ce, client, args))
	case "add":
		fnDeclinedAdd(ce, client, args)
	case "remove", "delete":
		fnDeclinedRemove(ce, client, args)
	case "purge":
		fnDeclinedPurge(ce, client)
	case "archive":
		fnDeclinedArchive(ce, client, args)
	case "leave-tg", "leave":
		fnDeclinedLeaveTG(ce, client, args)
	case "panel":
		fnDeclinedPanel(ce, client)
	case "help":
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix declined [list|add|remove|purge|archive|leave-tg|panel]`", "Использование: `$cmdprefix declined [list|add|remove|purge|archive|leave-tg|panel]`"))
	default:
		ce.Reply(trfEvent(ce, "Unknown subcommand %q", "Неизвестная подкоманда %q"), sub)
	}
}

func parseUnlimitedFlag(args []string) (limit int, rest []string) {
	limit = 10
	for i := 0; i < len(args); i++ {
		if (args[i] == "-n" || args[i] == "--limit") && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				limit = n
			}
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return
}

func declinedListText(ce *commands.Event, client *TelegramClient, args []string) string {
	limit, rest := parseUnlimitedFlag(args)
	keyword := strings.Join(rest, " ")
	rows, total, err := client.main.Store.ChatRejection.List(
		ce.Ctx, client.telegramUserID, keyword, store.SortRecent, limit, 0,
	)
	if err != nil {
		return trfEvent(ce, "Failed to list declined chats: %v", "Не удалось получить список отклонённых чатов: %v", err)
	}
	if total == 0 {
		if keyword != "" {
			return trfEvent(ce, "No declined chats match «%s».", "Нет отклонённых чатов по запросу «%s».", keyword)
		}
		return trEvent(ce, "No declined chats.", "Отклонённых чатов нет.")
	}
	var b strings.Builder
	b.WriteString(trfEvent(ce, "**Declined chats** — %d\n", "**Отклонённые чаты** — %d\n", total))
	for i, row := range rows {
		title := row.Title
		if title == "" {
			title = trEvent(ce, "(title unknown)", "(название неизвестно)")
		}
		fmt.Fprintf(&b, "%d. «%s» · `%s`", i+1, title, row.ID)
		if row.Participants > 0 {
			fmt.Fprintf(&b, " · %d %s", row.Participants, trEvent(ce, "members", "уч."))
		}
		if row.Reason != "" {
			fmt.Fprintf(&b, " · _%s_", row.Reason)
		}
		if row.LastSeenTS > 0 {
			fmt.Fprintf(&b, " · %s", time.Unix(row.LastSeenTS, 0).Format("02.01 15:04"))
		}
		if row.HasMXID {
			b.WriteString(" · " + trEvent(ce, "room exists", "комната есть"))
		} else {
			b.WriteString(" · " + trEvent(ce, "no room", "комнаты нет"))
		}
		if row.Source == store.RejectionSourceManual {
			b.WriteString(" · " + trEvent(ce, "manual", "вручную"))
		}
		b.WriteString("\n")
	}
	if total > len(rows) {
		fmt.Fprintf(&b, "\n%s", trfEvent(ce, "…and %d more (use `-n N` to show more)", "…и ещё %d (используй `-n N`)", total-len(rows)))
	}
	b.WriteString("\n" + trEvent(ce, "Interactive panel: `$cmdprefix declined panel`", "Интерактивная панель: `$cmdprefix declined panel`"))
	return b.String()
}

// resolveChatArg accepts "channel:123", "123", "chat:123", "user:123" or a
// keyword that matches exactly one catalog/rejection row.
func (client *TelegramClient) resolveChatArg(ctx context.Context, arg string) (networkid.PortalID, []string, error) {
	if id, ok := parseChatID(arg); ok {
		return id, nil, nil
	}
	catalog, _, err := client.main.Store.ChatCatalog.Search(ctx, client.telegramUserID, arg, store.SortRecent, 6, 0)
	if err != nil {
		return "", nil, err
	}
	rejections, _, err := client.main.Store.ChatRejection.List(ctx, client.telegramUserID, arg, store.SortRecent, 6, 0)
	if err != nil {
		return "", nil, err
	}
	seen := make(map[string]bool, len(catalog)+len(rejections))
	candidates := make([]string, 0, 6)
	for _, row := range catalog {
		if !seen[row.PortalID] {
			seen[row.PortalID] = true
			candidates = append(candidates, row.PortalID)
		}
	}
	for _, row := range rejections {
		if !seen[row.ID] {
			seen[row.ID] = true
			candidates = append(candidates, row.ID)
		}
	}
	if len(candidates) == 1 {
		return networkid.PortalID(candidates[0]), nil, nil
	}
	if len(candidates) == 0 {
		return "", nil, errors.New("not found")
	}
	return "", candidates, nil
}

func parseChatID(arg string) (networkid.PortalID, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", false
	}
	if _, err := strconv.ParseInt(arg, 10, 64); err == nil {
		return ids.MakePortalID(ids.PeerTypeChannel, mustParseInt(arg)), true
	}
	peerType, id, topicID, err := ids.ParsePortalID(networkid.PortalID(arg))
	if err != nil {
		return "", false
	}
	_ = topicID
	return ids.MakePortalID(peerType, id), true
}

func mustParseInt(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func fnDeclinedAdd(ce *commands.Event, client *TelegramClient, args []string) {
	if len(args) == 0 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix declined add <id|keyword>`", "Использование: `$cmdprefix declined add <id|слово>`"))
		return
	}
	portalID, candidates, err := client.resolveChatArg(ce.Ctx, strings.Join(args, " "))
	if err != nil {
		fnDeclinedResolveError(ce, args, candidates, err)
		return
	}
	row, _ := client.main.Store.ChatCatalog.Get(ce.Ctx, client.telegramUserID, string(portalID))
	hasMXID := client.portalMXID(ce.Ctx, string(portalID)) != ""
	if row == nil || row.Title == "" {
		if snap, err := client.fetchChatSnapshot(ce.Ctx, portalID); err == nil && snap.Kind != "" {
			client.recordCatalog(ce.Ctx, portalID, snap, nil)
		}
	}
	reason := trEvent(ce, "declined manually", "отклонён вручную")
	if err = client.rejectPeerManually(ce.Ctx, portalID, reason, hasMXID); err != nil {
		ce.Reply(trfEvent(ce, "Failed to decline %s: %v", "Не удалось отклонить %s: %v"), format.SafeMarkdownCode(string(portalID)), err)
		return
	}
	ce.Reply(trfEvent(ce, "Chat %s is now declined.", "Чат %s теперь отклонён."), format.SafeMarkdownCode(string(portalID)))
}

func fnDeclinedResolveError(ce *commands.Event, args, candidates []string, err error) {
	if len(candidates) > 0 {
		var b strings.Builder
		b.WriteString(trfEvent(ce, "Several chats match «%s», specify the ID:", "По запросу «%s» найдено несколько чатов, укажи ID:"))
		for _, candidate := range candidates {
			fmt.Fprintf(&b, "\n- `%s`", candidate)
		}
		ce.Reply(b.String(), strings.Join(args, " "))
		return
	}
	ce.Reply(trfEvent(ce, "Chat «%s» not found: %v", "Чат «%s» не найден: %v"), strings.Join(args, " "), err)
}

func fnDeclinedRemove(ce *commands.Event, client *TelegramClient, args []string) {
	if len(args) == 0 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix declined remove <id|keyword>`", "Использование: `$cmdprefix declined remove <id|слово>`"))
		return
	}
	portalID, candidates, err := client.resolveChatArg(ce.Ctx, strings.Join(args, " "))
	if err != nil {
		fnDeclinedResolveError(ce, args, candidates, err)
		return
	}
	if err = client.clearPeerRejection(ce.Ctx, portalID); err != nil {
		ce.Reply(trfEvent(ce, "Failed to restore %s: %v", "Не удалось восстановить %s: %v"), format.SafeMarkdownCode(string(portalID)), err)
		return
	}
	ce.Reply(trfEvent(ce, "Chat %s is no longer declined.", "Чат %s больше не отклонён."), format.SafeMarkdownCode(string(portalID)))
}

func fnDeclinedPurge(ce *commands.Event, client *TelegramClient) {
	rows, err := client.main.Store.ChatRejection.ListManual(ce.Ctx, client.telegramUserID)
	if err != nil {
		ce.Reply(trfEvent(ce, "Failed to list manual rejections: %v", "Не удалось получить ручные отклонения: %v"), err)
		return
	}
	for _, row := range rows {
		client.UnrejectPeer(networkid.PortalID(row.ID))
	}
	deleted, err := client.main.Store.ChatRejection.Purge(ce.Ctx, client.telegramUserID)
	if err != nil {
		ce.Reply(trfEvent(ce, "Failed to purge manual rejections: %v", "Не удалось удалить ручные отклонения: %v"), err)
		return
	}
	ce.Reply(trfEvent(ce, "Removed %d manual rejections.", "Удалено ручных отклонений: %d."), deleted)
}

func fnDeclinedArchive(ce *commands.Event, client *TelegramClient, args []string) {
	if len(args) == 0 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix declined archive <id>`", "Использование: `$cmdprefix declined archive <id>`"))
		return
	}
	portalID, candidates, err := client.resolveChatArg(ce.Ctx, strings.Join(args, " "))
	if err != nil {
		fnDeclinedResolveError(ce, args, candidates, err)
		return
	}
	roomID := client.portalMXID(ce.Ctx, string(portalID))
	if roomID == "" {
		ce.Reply(trfEvent(ce, "Chat %s has no Matrix room to archive.", "У чата %s нет комнаты Matrix для архивации."), format.SafeMarkdownCode(string(portalID)))
		return
	}
	doublePuppet := ce.User.DoublePuppet(ce.Ctx)
	if doublePuppet == nil {
		ce.Reply(trEvent(ce, "Double puppeting is required to archive rooms.", "Для архивации комнат нужен double puppeting."))
		return
	}
	if err = doublePuppet.TagRoom(ce.Ctx, roomID, event.RoomTagLowPriority, true); err != nil {
		ce.Reply(trfEvent(ce, "Failed to archive the room: %v", "Не удалось архивировать комнату: %v"), err)
		return
	}
	ce.Reply(trfEvent(ce, "Room %s moved to the archive.", "Комната %s убрана в архив."), format.SafeMarkdownCode(string(roomID)))
}

func fnDeclinedLeaveTG(ce *commands.Event, client *TelegramClient, args []string) {
	if len(args) == 0 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix declined leave-tg <id>` — this leaves the chat on Telegram permanently.", "Использование: `$cmdprefix declined leave-tg <id>` — это безвозвратно покидает чат в Telegram."))
		return
	}
	portalID, candidates, err := client.resolveChatArg(ce.Ctx, strings.Join(args, " "))
	if err != nil {
		fnDeclinedResolveError(ce, args, candidates, err)
		return
	}
	peerType, id, _, err := ids.ParsePortalID(portalID)
	if err != nil || (peerType != ids.PeerTypeChannel && peerType != ids.PeerTypeChat) {
		ce.Reply(trEvent(ce, "You can only leave groups and channels.", "Покинуть можно только группы и каналы."))
		return
	}
	if peerType == ids.PeerTypeChannel {
		accessHash, err := client.ScopedStore.GetAccessHash(ce.Ctx, ids.PeerTypeChannel, id)
		if err != nil {
			ce.Reply(trfEvent(ce, "Failed to get the channel access hash: %v", "Не удалось получить access hash канала: %v"), err)
			return
		}
		if _, err = client.client.API().ChannelsLeaveChannel(ce.Ctx, &tg.InputChannel{ChannelID: id, AccessHash: accessHash}); err != nil {
			ce.Reply(trfEvent(ce, "Failed to leave the channel: %v", "Не удалось покинуть канал: %v"), err)
			return
		}
	} else {
		ce.Reply(trEvent(ce, "Leaving legacy groups is not supported, leave it in a Telegram client.", "Выход из обычных групп не поддерживается, сделай это в клиенте Telegram."))
		return
	}
	ce.Reply(trfEvent(ce, "Left %s on Telegram.", "Покинут чат %s в Telegram."), format.SafeMarkdownCode(string(portalID)))
}

func fnDeclinedPanel(ce *commands.Event, client *TelegramClient) {
	roomID := ce.User.ManagementRoom
	if roomID == "" {
		roomID = ce.RoomID
	}
	state, err := client.main.Store.PanelState.GetOrCreate(ce.Ctx, ce.User.MXID, roomID)
	if err != nil {
		ce.Reply(trfEvent(ce, "Failed to load the panel state: %v", "Не удалось загрузить состояние панели: %v"), err)
		return
	}
	state.FilterQuery = ""
	state.Page = 0
	if err = client.drawPanel(ce.Ctx, ce.User, state, ""); err != nil {
		ce.Reply(trfEvent(ce, "Failed to draw the panel: %v", "Не удалось нарисовать панель: %v"), err)
	}
}

func fnPanelSearch(ce *commands.Event) {
	client, _ := loginClientForEvent(ce)
	if client == nil {
		ce.Reply(trEvent(ce, "You are not logged in to Telegram", "Вы не авторизованы в Telegram"))
		return
	}
	args := ce.Args
	if len(args) == 0 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix s <query>` or `$cmdprefix s --refresh <id>`", "Использование: `$cmdprefix s <запрос>` или `$cmdprefix s --refresh <id>`"))
		return
	}
	if args[0] == "--refresh" {
		if len(args) < 2 {
			ce.Reply(trEvent(ce, "Usage: `$cmdprefix s --refresh <id>`", "Использование: `$cmdprefix s --refresh <id>`"))
			return
		}
		portalID, candidates, err := client.resolveChatArg(ce.Ctx, strings.Join(args[1:], " "))
		if err != nil {
			fnDeclinedResolveError(ce, args[1:], candidates, err)
			return
		}
		about, err := client.fetchCatalogAbout(ce.Ctx, portalID, true)
		if err != nil {
			ce.Reply(trfEvent(ce, "Failed to refresh %s: %v", "Не удалось обновить %s: %v"), format.SafeMarkdownCode(string(portalID)), err)
			return
		}
		if about == "" {
			ce.Reply(trfEvent(ce, "%s refreshed, it has no description.", "%s обновлён, у чата нет описания."), format.SafeMarkdownCode(string(portalID)))
		} else {
			ce.Reply(trfEvent(ce, "%s refreshed: %s", "%s обновлён: %s"), format.SafeMarkdownCode(string(portalID)), about)
		}
		return
	}
	roomID := ce.User.ManagementRoom
	if roomID == "" {
		roomID = ce.RoomID
	}
	state, err := client.main.Store.PanelState.GetOrCreate(ce.Ctx, ce.User.MXID, roomID)
	if err != nil {
		ce.Reply(trfEvent(ce, "Failed to load the panel state: %v", "Не удалось загрузить состояние панели: %v"), err)
		return
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "." || query == "*" {
		query = ""
	}
	state.FilterQuery = query
	state.Page = 0
	if err = client.drawPanel(ce.Ctx, ce.User, state, ""); err != nil {
		ce.Reply(trfEvent(ce, "Failed to draw the panel: %v", "Не удалось нарисовать панель: %v"), err)
	}
}

func fnPanelDigit(ce *commands.Event) {
	number, ok := parsePanelNumber(ce.Command)
	if !ok {
		return
	}
	client, _ := loginClientForEvent(ce)
	if client == nil {
		return
	}
	ce.MessageStatus.DisableMSS = true
	if ce.User.ManagementRoom == "" || ce.RoomID != ce.User.ManagementRoom {
		ce.Reply(trEvent(ce, "Row numbers only work in the management room.", "Номера строк работают только в комнате управления."))
		return
	}
	ce.Redact()
	go client.selectAbsoluteRow(context.WithoutCancel(ce.Ctx), ce.User, number)
}
