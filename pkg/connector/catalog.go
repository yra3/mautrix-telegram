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
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/connector/store"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

// catalogSnapshot is the chat metadata known at the moment a dialog or an
// update is processed. Empty fields mean "unknown, keep the stored value".
type catalogSnapshot struct {
	Kind         string
	Title        string
	About        string
	Username     string
	Participants int
	IsForum      bool
	IsBroadcast  bool
	// AboutKnown marks a snapshot that came from a full-chat response, so an
	// empty About means "this chat has no description", not "not fetched yet".
	AboutKnown bool
}

func catalogKindForChat(chat tg.ChatClass) string {
	switch typed := chat.(type) {
	case *tg.Chat:
		return "group"
	case *tg.Channel:
		if typed.Broadcast {
			return "channel"
		} else if typed.Forum {
			return "forum"
		}
		return "group"
	}
	return ""
}

func snapshotFromChat(chat tg.ChatClass) catalogSnapshot {
	switch typed := chat.(type) {
	case *tg.Chat:
		return catalogSnapshot{
			Kind:         "group",
			Title:        typed.Title,
			Participants: typed.ParticipantsCount,
		}
	case *tg.Channel:
		return catalogSnapshot{
			Kind:         catalogKindForChat(typed),
			Title:        typed.Title,
			Username:     typed.Username,
			Participants: typed.ParticipantsCount,
			IsForum:      typed.Forum,
			IsBroadcast:  typed.Broadcast,
		}
	}
	return catalogSnapshot{}
}

func snapshotFromUser(user *tg.User) catalogSnapshot {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name == "" {
		name = user.Username
	}
	return catalogSnapshot{
		Kind:     "user",
		Title:    name,
		Username: user.Username,
	}
}

// recordCatalog writes a snapshot into chat_catalog. Errors are logged and
// swallowed: the catalog is an auxiliary index, it must never break bridging.
func (tc *TelegramClient) recordCatalog(ctx context.Context, portalID networkid.PortalID, snap catalogSnapshot, inArchive *bool) {
	if portalID == "" {
		return
	}
	patch := store.CatalogPatch{
		Title:        &snap.Title,
		Username:     &snap.Username,
		Participants: &snap.Participants,
		MetaFetched:  true,
		InArchive:    inArchive,
	}
	if snap.Kind != "" {
		patch.Kind = &snap.Kind
	}
	if snap.AboutKnown || snap.About != "" {
		patch.About = &snap.About
		patch.AboutFetched = true
	}
	if snap.IsForum || snap.IsBroadcast {
		patch.IsForum = &snap.IsForum
		patch.IsBroadcast = &snap.IsBroadcast
	}
	err := tc.main.Store.ChatCatalog.Update(ctx, tc.telegramUserID, string(portalID), patch)
	if err != nil && !errors.Is(err, context.Canceled) {
		zerolog.Ctx(ctx).Warn().Err(err).
			Str("portal_id", string(portalID)).
			Msg("Failed to update chat catalog")
	}
}

// recordCatalogFromDialog stores a dialog as soon as it is seen, including
// dialogs that are later rejected (archived, bots, too many participants).
func (tc *TelegramClient) recordCatalogFromDialog(
	ctx context.Context, dialog tg.DialogClass, users map[int64]tg.UserClass, chats map[int64]tg.ChatClass,
) {
	var peer tg.PeerClass
	var inArchive bool
	switch typed := dialog.(type) {
	case *tg.Dialog:
		peer = typed.Peer
		folderID, _ := typed.GetFolderID()
		inArchive = folderID != 0
	case *tg.DialogFolder:
		peer = typed.Peer
		inArchive = true
	default:
		return
	}
	if peer == nil {
		return
	}
	var snap catalogSnapshot
	switch typedPeer := peer.(type) {
	case *tg.PeerUser:
		if user, ok := users[typedPeer.UserID].(*tg.User); ok {
			snap = snapshotFromUser(user)
		}
	case *tg.PeerChat:
		if chat, ok := chats[typedPeer.ChatID]; ok {
			snap = snapshotFromChat(chat)
		}
	case *tg.PeerChannel:
		if chat, ok := chats[typedPeer.ChannelID]; ok {
			snap = snapshotFromChat(chat)
		}
	}
	if snap.Kind == "" {
		return
	}
	tc.recordCatalog(ctx, tc.dialogToPortalKey(dialog).ID, snap, &inArchive)
}

// updateCatalogFromEntity updates the catalog from a bare Telegram entity
// without any extra API calls (used by update handlers).
func (tc *TelegramClient) updateCatalogFromEntity(ctx context.Context, portalID networkid.PortalID, entity tg.ChatClass) {
	snap := snapshotFromChat(entity)
	if snap.Kind == "" {
		return
	}
	tc.recordCatalog(ctx, portalID, snap, nil)
}

// fetchCatalogAbout lazily loads the chat description (about) for a catalog
// row. It is idempotent: once about_fetched_ts is set the row is not fetched
// again unless force is true.
func (tc *TelegramClient) fetchCatalogAbout(ctx context.Context, portalID networkid.PortalID, force bool) (string, error) {
	row, err := tc.main.Store.ChatCatalog.Get(ctx, tc.telegramUserID, string(portalID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if row != nil && row.AboutFetchedTS != 0 && !force {
		return row.About, nil
	}
	peerType, id, _, err := ids.ParsePortalID(portalID)
	if err != nil {
		return "", err
	}
	var about string
	switch peerType {
	case ids.PeerTypeChannel:
		accessHash, err := tc.ScopedStore.GetAccessHash(ctx, ids.PeerTypeChannel, id)
		if err != nil {
			return "", fmt.Errorf("failed to get channel access hash: %w", err)
		}
		fullChat, err := APICallWithUpdates(ctx, tc, func() (*tg.MessagesChatFull, error) {
			return tc.client.API().ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: id, AccessHash: accessHash})
		})
		if err != nil {
			return "", err
		}
		about = fullChat.FullChat.GetAbout()
	case ids.PeerTypeChat:
		fullChat, err := APICallWithUpdates(ctx, tc, func() (*tg.MessagesChatFull, error) {
			return tc.client.API().MessagesGetFullChat(ctx, id)
		})
		if err != nil {
			return "", err
		}
		about = fullChat.FullChat.GetAbout()
	default:
		// Users have no public about field.
		about = ""
	}
	about = strings.TrimSpace(about)
	patch := store.CatalogPatch{About: &about, AboutFetched: true}
	err = tc.main.Store.ChatCatalog.Update(ctx, tc.telegramUserID, string(portalID), patch)
	if err != nil {
		return about, err
	}
	return about, nil
}

// pendingAboutFetches deduplicates background about fetches.
var pendingAboutFetches sync.Map

// fetchCatalogAboutAsync loads the about field in the background, at most once
// per portal at a time.
func (tc *TelegramClient) fetchCatalogAboutAsync(ctx context.Context, portalID networkid.PortalID) {
	if portalID == "" {
		return
	}
	if _, loaded := pendingAboutFetches.LoadOrStore(string(portalID), struct{}{}); loaded {
		return
	}
	go func() {
		defer pendingAboutFetches.Delete(string(portalID))
		// The caller's context may be tied to a single event, so use the client context.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := tc.fetchCatalogAbout(fetchCtx, portalID, false); err != nil {
			zerolog.Ctx(fetchCtx).Debug().Err(err).
				Str("portal_id", string(portalID)).
				Msg("Failed to fetch chat about")
		}
	}()
}

// fetchCatalogSnapshotAsync fills a catalog row that the panel shows but that
// has no snapshot yet (the rejection happened before the catalog knew the chat).
// It also loads about, so one call per row is enough.
func (tc *TelegramClient) fetchCatalogSnapshotAsync(ctx context.Context, portalID networkid.PortalID) {
	if portalID == "" {
		return
	}
	if _, loaded := pendingAboutFetches.LoadOrStore(string(portalID), struct{}{}); loaded {
		return
	}
	go func() {
		defer pendingAboutFetches.Delete(string(portalID))
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		snap, err := tc.fetchChatSnapshot(fetchCtx, portalID)
		if err != nil || snap.Kind == "" {
			zerolog.Ctx(fetchCtx).Debug().Err(err).
				Str("portal_id", string(portalID)).
				Msg("Failed to fetch chat snapshot")
			return
		}
		tc.recordCatalog(fetchCtx, portalID, snap, nil)
	}()
}

// fetchChatSnapshot fetches chat metadata with a single API call. It is used
// when a chat is added manually and is not in the catalog yet.
func (tc *TelegramClient) fetchChatSnapshot(ctx context.Context, portalID networkid.PortalID) (catalogSnapshot, error) {
	peerType, id, _, err := ids.ParsePortalID(portalID)
	if err != nil {
		return catalogSnapshot{}, err
	}
	switch peerType {
	case ids.PeerTypeUser:
		user, err := tc.getSingleUser(ctx, id)
		if err != nil {
			return catalogSnapshot{}, err
		}
		if typed, ok := user.(*tg.User); ok {
			return snapshotFromUser(typed), nil
		}
		return catalogSnapshot{}, fmt.Errorf("user %d is %T, not *tg.User", id, user)
	case ids.PeerTypeChat:
		fullChat, err := APICallWithUpdates(ctx, tc, func() (*tg.MessagesChatFull, error) {
			return tc.client.API().MessagesGetFullChat(ctx, id)
		})
		if err != nil {
			return catalogSnapshot{}, err
		}
		return snapshotFromFullChat(fullChat), nil
	case ids.PeerTypeChannel:
		accessHash, err := tc.ScopedStore.GetAccessHash(ctx, ids.PeerTypeChannel, id)
		if err != nil {
			return catalogSnapshot{}, fmt.Errorf("failed to get channel access hash: %w", err)
		}
		fullChat, err := APICallWithUpdates(ctx, tc, func() (*tg.MessagesChatFull, error) {
			return tc.client.API().ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: id, AccessHash: accessHash})
		})
		if err != nil {
			return catalogSnapshot{}, err
		}
		return snapshotFromFullChat(fullChat), nil
	}
	return catalogSnapshot{}, fmt.Errorf("unsupported peer type %s", peerType)
}

func snapshotFromFullChat(fullChat *tg.MessagesChatFull) catalogSnapshot {
	var snap catalogSnapshot
	for _, chat := range fullChat.GetChats() {
		if chat.GetID() == fullChat.FullChat.GetID() {
			snap = snapshotFromChat(chat)
			break
		}
	}
	snap.About = strings.TrimSpace(fullChat.FullChat.GetAbout())
	snap.AboutKnown = true
	return snap
}
