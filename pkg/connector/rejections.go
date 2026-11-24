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
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-telegram/pkg/connector/store"
)

// rejectionLogInterval limits how often the same rejected peer is logged at
// Info level, so a busy channel cannot flood the log.
const rejectionLogInterval = 60 * time.Second

// isRejectedLogAllowed reports whether the peer may be logged now. Callers
// must not hold rejectedLogLock.
func (tc *TelegramClient) isRejectedLogAllowed(portalID networkid.PortalID) bool {
	now := time.Now()
	tc.rejectedLogLock.Lock()
	defer tc.rejectedLogLock.Unlock()
	if tc.rejectedLogTS == nil {
		tc.rejectedLogTS = make(map[networkid.PortalID]time.Time)
	}
	if last, ok := tc.rejectedLogTS[portalID]; ok && now.Sub(last) < rejectionLogInterval {
		return false
	}
	tc.rejectedLogTS[portalID] = now
	return true
}

// logRejectedPeer logs an Info line for a rejected peer at most once a minute.
func (tc *TelegramClient) logRejectedPeer(ctx context.Context, portalID networkid.PortalID, reason string, hasMXID bool) {
	if !tc.isRejectedLogAllowed(portalID) {
		return
	}
	zerolog.Ctx(ctx).Info().
		Str("portal_id", string(portalID)).
		Bool("has_mxid", hasMXID).
		Str("reason", reason).
		Msg("Rejected Telegram chat")
}

// rejectionPatch builds a decision snapshot for a rejection from the catalog.
func (tc *TelegramClient) rejectionPatch(ctx context.Context, portalID networkid.PortalID, reason string, hasMXID bool) store.PatchRejection {
	patch := store.PatchRejection{Reason: reason, HasMXID: hasMXID}
	row, err := tc.main.Store.ChatCatalog.Get(ctx, tc.telegramUserID, string(portalID))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			zerolog.Ctx(ctx).Debug().Err(err).Msg("Failed to read catalog row for rejection snapshot")
		}
		return patch
	}
	patch.Title = row.Title
	patch.About = row.About
	patch.Participants = row.Participants
	return patch
}

// rejectPeer records an automatic rejection in memory and in the database.
func (tc *TelegramClient) rejectPeer(ctx context.Context, portalID networkid.PortalID, reason string, hasMXID bool) {
	if portalID == "" {
		return
	}
	tc.RejectPeer(portalID, reason)
	tc.logRejectedPeer(ctx, portalID, reason, hasMXID)
	err := tc.main.Store.ChatRejection.Upsert(
		ctx, tc.telegramUserID, string(portalID), store.RejectionSourceAuto,
		tc.rejectionPatch(ctx, portalID, reason, hasMXID),
	)
	if err != nil && !errors.Is(err, context.Canceled) {
		zerolog.Ctx(ctx).Warn().Err(err).
			Str("portal_id", string(portalID)).
			Msg("Failed to persist automatic chat rejection")
	}
}

// rejectPeerManually records a user-requested rejection that survives restarts.
func (tc *TelegramClient) rejectPeerManually(ctx context.Context, portalID networkid.PortalID, reason string, hasMXID bool) error {
	if portalID == "" {
		return errors.New("empty portal ID")
	}
	tc.RejectPeerManual(portalID, reason)
	tc.logRejectedPeer(ctx, portalID, reason, hasMXID)
	return tc.main.Store.ChatRejection.Upsert(
		ctx, tc.telegramUserID, string(portalID), store.RejectionSourceManual,
		tc.rejectionPatch(ctx, portalID, reason, hasMXID),
	)
}

// clearPeerRejection removes a rejection from the cache and from both the auto
// and the manual database rows.
func (tc *TelegramClient) clearPeerRejection(ctx context.Context, portalID networkid.PortalID) error {
	if portalID == "" {
		return nil
	}
	tc.UnrejectPeer(portalID)
	for _, source := range []string{store.RejectionSourceManual, store.RejectionSourceAuto} {
		if _, err := tc.main.Store.ChatRejection.Delete(ctx, tc.telegramUserID, string(portalID), source); err != nil {
			return err
		}
	}
	return nil
}

// restoreManualRejections puts every persisted manual rejection back into the
// in-memory cache. It runs once per client start.
func (tc *TelegramClient) restoreManualRejections(ctx context.Context) {
	rows, err := tc.main.Store.ChatRejection.ListManual(ctx, tc.telegramUserID)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to load persisted chat rejections")
		return
	}
	for _, row := range rows {
		tc.RejectPeerManual(networkid.PortalID(row.ID), row.Reason)
	}
	if len(rows) > 0 {
		zerolog.Ctx(ctx).Info().Int("count", len(rows)).Msg("Restored persisted chat rejections")
	}
}
