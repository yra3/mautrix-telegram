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

package store

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/util/dbutil"
)

const (
	RejectionSourceAuto   = "auto"
	RejectionSourceManual = "manual"
)

type ChatRejectionRow struct {
	UserID       int64
	ID           string
	Source       string
	Reason       string
	Title        string
	About        string
	Participants int
	FirstSeenTS  int64
	LastSeenTS   int64
	HasMXID      bool

	// Filled in from chat_catalog by the list queries.
	Username  string
	InArchive bool
	Kind      string
}

type ChatRejectionQuery struct {
	db *dbutil.Database
}

const (
	chatRejectionColumns = `user_id, id, source, reason, title, about, participants,
		first_seen_ts, last_seen_ts, has_mxid`

	listChatRejectionsQuery = "SELECT " + chatRejectionColumns + " FROM chat_rejection WHERE user_id=$1"
	getChatRejectionQuery   = "SELECT " + chatRejectionColumns + " FROM chat_rejection WHERE user_id=$1 AND id=$2"
	listManualRejectionsQ   = "SELECT " + chatRejectionColumns + ` FROM chat_rejection
		WHERE user_id=$1 AND source='` + RejectionSourceManual + `'`
	upsertChatRejectionQuery = `
		INSERT INTO chat_rejection (` + chatRejectionColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_id, id, source) DO UPDATE SET
			reason=$4, title=$5, about=$6, participants=$7,
			first_seen_ts=$8, last_seen_ts=$9, has_mxid=$10
	`
	deleteChatRejectionQuery = "DELETE FROM chat_rejection WHERE user_id=$1 AND id=$2 AND source=$3"
	purgeManualRejectionsQ   = "DELETE FROM chat_rejection WHERE user_id=$1 AND source=$2"
)

func scanChatRejectionRow(row dbutil.Scannable) (*ChatRejectionRow, error) {
	var r ChatRejectionRow
	var hasMXID int
	err := row.Scan(
		&r.UserID, &r.ID, &r.Source, &r.Reason, &r.Title, &r.About, &r.Participants,
		&r.FirstSeenTS, &r.LastSeenTS, &hasMXID,
	)
	if err != nil {
		return nil, err
	}
	r.HasMXID = hasMXID != 0
	return &r, nil
}

func (r *ChatRejectionRow) sqlVariables() []any {
	return []any{
		r.UserID, r.ID, r.Source, r.Reason, r.Title, r.About, r.Participants,
		r.FirstSeenTS, r.LastSeenTS, boolToInt(r.HasMXID),
	}
}

// PatchRejection describes a new sighting of a rejected chat. Empty fields are
// filled from the previous row, so a rejection never loses its snapshot.
type PatchRejection struct {
	Reason       string
	Title        string
	About        string
	Participants int
	HasMXID      bool
}

// Upsert records a rejection sighting. source is RejectionSourceAuto or
// RejectionSourceManual. Auto rows keep their first_seen timestamp until the
// rejection is removed.
func (q *ChatRejectionQuery) Upsert(ctx context.Context, userID int64, id, source string, patch PatchRejection) error {
	now := time.Now().Unix()
	existing := q.getOne(ctx, userID, id, source)
	row := ChatRejectionRow{
		UserID:       userID,
		ID:           id,
		Source:       source,
		Reason:       patch.Reason,
		Title:        patch.Title,
		About:        patch.About,
		Participants: patch.Participants,
		HasMXID:      patch.HasMXID,
		FirstSeenTS:  now,
		LastSeenTS:   now,
	}
	if existing != nil {
		row.FirstSeenTS = existing.FirstSeenTS
		if row.Reason == "" {
			row.Reason = existing.Reason
		}
		if row.Title == "" {
			row.Title = existing.Title
		}
		if row.About == "" {
			row.About = existing.About
		}
		if row.Participants == 0 {
			row.Participants = existing.Participants
		}
		if !row.HasMXID {
			row.HasMXID = existing.HasMXID
		}
	}
	_, err := q.db.Exec(ctx, upsertChatRejectionQuery, row.sqlVariables()...)
	return err
}

func (q *ChatRejectionQuery) getOne(ctx context.Context, userID int64, id, source string) *ChatRejectionRow {
	rows, err := dbutil.ConvertRowFn[*ChatRejectionRow](scanChatRejectionRow).
		NewRowIter(q.db.Query(ctx, getChatRejectionQuery, userID, id)).AsList()
	if err != nil {
		return nil
	}
	for _, row := range rows {
		if row.Source == source {
			return row
		}
	}
	return nil
}

// Get returns every rejection row of a portal (auto and manual).
func (q *ChatRejectionQuery) Get(ctx context.Context, userID int64, id string) ([]*ChatRejectionRow, error) {
	return dbutil.ConvertRowFn[*ChatRejectionRow](scanChatRejectionRow).
		NewRowIter(q.db.Query(ctx, getChatRejectionQuery, userID, id)).AsList()
}

// GetOne returns the manual row if it exists, otherwise the auto row.
func (q *ChatRejectionQuery) GetOne(ctx context.Context, userID int64, id string) *ChatRejectionRow {
	rows, err := q.Get(ctx, userID, id)
	if err != nil {
		return nil
	}
	var auto *ChatRejectionRow
	for _, row := range rows {
		if row.Source == RejectionSourceManual {
			return row
		}
		auto = row
	}
	return auto
}

func (q *ChatRejectionQuery) Delete(ctx context.Context, userID int64, id, source string) (int64, error) {
	res, err := q.db.Exec(ctx, deleteChatRejectionQuery, userID, id, source)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Purge removes every manual rejection and returns the number of deleted rows.
func (q *ChatRejectionQuery) Purge(ctx context.Context, userID int64) (int64, error) {
	res, err := q.db.Exec(ctx, purgeManualRejectionsQ, userID, RejectionSourceManual)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (q *ChatRejectionQuery) ListManual(ctx context.Context, userID int64) ([]*ChatRejectionRow, error) {
	return dbutil.ConvertRowFn[*ChatRejectionRow](scanChatRejectionRow).
		NewRowIter(q.db.Query(ctx, listManualRejectionsQ, userID)).AsList()
}

// List returns the rejection list, optionally filtered by a keyword, with the
// catalog data merged in (titles and participants are used as a fallback).
func (q *ChatRejectionQuery) List(
	ctx context.Context, userID int64, keyword string, order CatalogSort, limit, offset int,
) ([]*ChatRejectionRow, int, error) {
	rows, err := dbutil.ConvertRowFn[*ChatRejectionRow](scanChatRejectionRow).
		NewRowIter(q.db.Query(ctx, listChatRejectionsQuery, userID)).AsList()
	if err != nil {
		return nil, 0, err
	}
	catalog, err := q.catalogMap(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	// One portal may have both an auto and a manual row: the manual one wins.
	byID := make(map[string]*ChatRejectionRow, len(rows))
	for _, row := range rows {
		if prev, ok := byID[row.ID]; ok && prev.Source == RejectionSourceManual {
			continue
		}
		if cat, ok := catalog[row.ID]; ok {
			if row.Title == "" {
				row.Title = cat.Title
			}
			if row.About == "" {
				row.About = cat.About
			}
			if row.Participants == 0 {
				row.Participants = cat.Participants
			}
			row.Username = cat.Username
			row.InArchive = cat.InArchive
			row.Kind = cat.Kind
		}
		byID[row.ID] = row
	}
	keyword = NormalizeSearch(strings.TrimPrefix(keyword, "@"))
	matches := make([]*ChatRejectionRow, 0, len(byID))
	scores := make(map[string]int, len(byID))
	for id, row := range byID {
		score := ScoreRejectionRow(row, keyword)
		if score <= 0 {
			continue
		}
		matches = append(matches, row)
		scores[id] = score
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if keyword != "" && scores[matches[i].ID] != scores[matches[j].ID] {
			return scores[matches[i].ID] > scores[matches[j].ID]
		}
		switch order {
		case SortSize:
			if matches[i].Participants != matches[j].Participants {
				return matches[i].Participants > matches[j].Participants
			}
		case SortAlpha:
			if !strings.EqualFold(matches[i].Title, matches[j].Title) {
				return strings.ToLower(matches[i].Title) < strings.ToLower(matches[j].Title)
			}
		}
		return matches[i].LastSeenTS > matches[j].LastSeenTS
	})
	total := len(matches)
	if offset < 0 {
		offset = 0
	}
	if offset < len(matches) {
		matches = matches[offset:]
	} else {
		matches = nil
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, total, nil
}

func (q *ChatRejectionQuery) catalogMap(ctx context.Context, userID int64) (map[string]*ChatCatalogRow, error) {
	rows, err := q.catalogList(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*ChatCatalogRow, len(rows))
	for _, row := range rows {
		out[row.PortalID] = row
	}
	return out, nil
}

func (q *ChatRejectionQuery) catalogList(ctx context.Context, userID int64) ([]*ChatCatalogRow, error) {
	return dbutil.ConvertRowFn[*ChatCatalogRow](scanChatCatalogRow).
		NewRowIter(q.db.Query(ctx, listChatCatalogQuery, userID)).AsList()
}

// ScoreRejectionRow returns the match quality for a normalized query, 0 = no match.
func ScoreRejectionRow(row *ChatRejectionRow, query string) int {
	if query == "" {
		return 1
	}
	title := NormalizeSearch(row.Title)
	about := NormalizeSearch(row.About)
	reason := NormalizeSearch(row.Reason)
	username := NormalizeSearch(strings.TrimPrefix(row.Username, "@"))
	portalID := NormalizeSearch(row.ID)
	_, numericID, _ := strings.Cut(row.ID, ":")
	switch {
	case portalID == query || numericID == query:
		return 100
	case username != "" && strings.HasPrefix(username, query):
		return 90
	case title != "" && title == query:
		return 80
	case strings.HasPrefix(title, query):
		return 70
	case strings.Contains(title, query):
		return 60
	case strings.Contains(reason, query):
		return 50
	case strings.Contains(about, query):
		return 40
	case row.Participants > 0 && strings.HasPrefix(strconv.Itoa(row.Participants), query):
		return 30
	case strings.Contains(portalID, query), numericID != "" && strings.Contains(numericID, query):
		return 20
	case strings.Contains(row.SearchBlob(), query):
		return 10
	}
	return 0
}

func (r *ChatRejectionRow) SearchBlob() string {
	var b strings.Builder
	for _, part := range []string{r.Title, r.About, r.Reason, strings.TrimPrefix(r.Username, "@"), r.ID} {
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(NormalizeSearch(part))
	}
	return b.String()
}
