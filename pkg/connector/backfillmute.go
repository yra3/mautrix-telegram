// mautrix-telegram - A Matrix-Telegram puppeting bridge.
// Copyright (C) 2025 Sumner Evans
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
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/jsontime"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

// backfillMuteMaxWait is how long a portal may stay muted for a backfill before the
// mute is force-restored. This is a safety net for the case where the completion
// callback never runs (backfill errored out, process restarted mid-backfill, ...):
// on Matrix servers without mute-until support a forgotten mute never expires by
// itself, so the room would silently stay muted forever.
const backfillMuteMaxWait = 30 * time.Minute

// shouldMuteForBackfill reports whether the backfill that is about to run is the
// initial one for a freshly created portal, which is the only case where the room
// should be muted temporarily.
func shouldMuteForBackfill(forward, hasAnchor bool, mxid id.RoomID, enabled bool) bool {
	return enabled && forward && !hasAnchor && mxid != ""
}

func (tc *TelegramClient) portalLoginsForMute(ctx context.Context, portal *bridgev2.Portal) []*bridgev2.UserLogin {
	logins, err := tc.main.Bridge.GetUserLoginsInPortal(ctx, portal.PortalKey)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to get logins in portal to sync notification settings")
		return nil
	}
	return logins
}

// mutePortalForBackfill mutes the room for every Matrix user that has double
// puppeting enabled, so the initial backfill messages don't produce notifications.
func (tc *TelegramClient) mutePortalForBackfill(ctx context.Context, portal *bridgev2.Portal) bool {
	log := zerolog.Ctx(ctx).With().Str("action", "mute_for_backfill").Logger()
	ctx = log.WithContext(ctx)
	muted := false
	for _, login := range tc.portalLoginsForMute(ctx, portal) {
		dp := login.User.DoublePuppet(ctx)
		if dp == nil {
			continue
		}
		err := dp.MuteRoom(ctx, portal.MXID, event.MutedForever)
		if err != nil {
			log.Err(err).Stringer("user_id", dp.GetMXID()).Msg("Failed to mute portal for backfill")
			continue
		}
		muted = true
	}
	if !muted {
		log.Debug().Msg("No double puppeted users to mute the portal for")
		return false
	}
	meta, ok := portal.Metadata.(*PortalMetadata)
	if !ok {
		log.Warn().Msg("Portal has no Telegram metadata, not tracking backfill mute")
		return false
	}
	meta.MutedForBackfill = jsontime.UnixNow()
	err := portal.Save(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to save portal metadata after muting for backfill")
	}
	time.AfterFunc(backfillMuteMaxWait, func() {
		tc.restorePortalMute(context.WithoutCancel(ctx), portal)
	})
	log.Info().Msg("Muted portal for initial backfill, Telegram notification settings will be applied afterwards")
	return true
}

// restorePortalMute applies the real Telegram notification settings to the room after
// the initial backfill has been sent, which also unmutes the room if the chat is
// unmuted on Telegram.
func (tc *TelegramClient) restorePortalMute(ctx context.Context, portal *bridgev2.Portal) {
	if portal == nil || portal.MXID == "" || portal.Metadata == nil {
		return
	}
	log := zerolog.Ctx(ctx).With().Str("action", "restore_mute_after_backfill").Logger()
	ctx = log.WithContext(ctx)
	meta, ok := portal.Metadata.(*PortalMetadata)
	if !ok {
		return
	}

	tc.backfillMuteLock.Lock()
	defer tc.backfillMuteLock.Unlock()
	if meta.MutedForBackfill.IsZero() {
		// Already restored by the completion callback or the startup sweep.
		return
	}
	defer func() {
		meta.MutedForBackfill = jsontime.Unix{}
		err := portal.Save(ctx)
		if err != nil {
			log.Err(err).Msg("Failed to save portal metadata after restoring notification settings")
		}
	}()

	info, err := tc.getPeerUserLocalInfo(ctx, portal)
	if err != nil || info == nil || info.UserLocal == nil {
		// Without the remote settings the room would stay muted forever, so unmute it
		// and let the next regular chat sync apply the real state.
		if err == nil {
			err = fmt.Errorf("no user-local info in peer dialog response")
		}
		log.Err(err).Msg("Failed to get Telegram notification settings after backfill, unmuting portal")
		tc.setPortalMuted(ctx, portal, false)
		return
	}
	portal.UpdateInfo(ctx, info, tc.userLogin, nil, time.Time{})
	log.Info().
		Bool("muted_in_telegram", info.UserLocal.MutedUntil != nil && info.UserLocal.MutedUntil.After(time.Now())).
		Msg("Applied Telegram notification settings after initial backfill")
}

// setPortalMuted force-sets the mute state of the room for all double puppeted users.
func (tc *TelegramClient) setPortalMuted(ctx context.Context, portal *bridgev2.Portal, muted bool) {
	until := time.Unix(0, 0)
	if muted {
		until = event.MutedForever
	}
	for _, login := range tc.portalLoginsForMute(ctx, portal) {
		dp := login.User.DoublePuppet(ctx)
		if dp == nil {
			continue
		}
		err := dp.MuteRoom(ctx, portal.MXID, until)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).
				Stringer("user_id", dp.GetMXID()).
				Bool("muted", muted).
				Msg("Failed to set portal mute state")
		}
	}
}

// getPeerUserLocalInfo fetches only the per-user local info (mute and pin state) of a
// portal's chat from Telegram.
func (tc *TelegramClient) getPeerUserLocalInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	inputPeer, _, err := tc.inputPeerForPortalID(ctx, portal.ID)
	if err != nil {
		return nil, err
	}
	resp, err := APICallWithUpdates(ctx, tc, func() (*tg.MessagesPeerDialogs, error) {
		return tc.client.API().MessagesGetPeerDialogs(ctx, []tg.InputDialogPeerClass{&tg.InputDialogPeer{Peer: inputPeer}})
	})
	if err != nil {
		return nil, err
	}
	for _, dialog := range resp.Dialogs {
		fullDialog, ok := dialog.(*tg.Dialog)
		if !ok {
			continue
		}
		info := &bridgev2.ChatInfo{}
		tc.fillUserLocalMeta(info, fullDialog)
		return info, nil
	}
	return nil, fmt.Errorf("peer dialog for %s not found", portal.ID)
}

// restoreStaleBackfillMutes unmutes portals that were left muted for a backfill when
// the bridge was stopped before the backfill finished.
func (tc *TelegramClient) restoreStaleBackfillMutes(ctx context.Context) {
	log := zerolog.Ctx(ctx).With().Str("action", "restore_stale_backfill_mutes").Logger()
	userPortals, err := tc.main.Bridge.DB.UserPortal.GetAllForLogin(ctx, tc.userLogin.UserLogin)
	if err != nil {
		log.Err(err).Msg("Failed to get user portals to restore backfill mutes")
		return
	}
	restored := 0
	for _, up := range userPortals {
		portal, err := tc.main.Bridge.GetExistingPortalByKey(ctx, up.Portal)
		if err != nil {
			log.Err(err).Msg("Failed to get portal to restore backfill mute")
			continue
		}
		if portal == nil || portal.MXID == "" || portal.Metadata == nil {
			continue
		}
		meta, ok := portal.Metadata.(*PortalMetadata)
		if !ok || meta.MutedForBackfill.IsZero() {
			continue
		}
		tc.restorePortalMute(log.WithContext(ctx), portal)
		restored++
	}
	if restored > 0 {
		log.Info().Int("count", restored).Msg("Restored notification settings of portals left muted by a backfill")
	}
}
