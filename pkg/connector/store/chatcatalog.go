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
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/util/dbutil"
)

// ChatCatalogRow is one known Telegram chat of one login. Title/about are the
// last known values, search_blob is the normalized text used for searching.
type ChatCatalogRow struct {
	UserID         int64
	PortalID       string
	Kind           string
	Title          string
	About          string
	Username       string
	Participants   int
	IsForum        bool
	IsBroadcast    bool
	InArchive      bool
	MetaFetchedTS  int64
	AboutFetchedTS int64
	SearchBlob     string
}

// CatalogPatch updates a single catalog row. Nil fields are left untouched.
type CatalogPatch struct {
	Kind         *string
	Title        *string
	About        *string
	Username     *string
	Participants *int
	IsForum      *bool
	IsBroadcast  *bool
	InArchive    *bool
	// MetaFetched marks the title/username/participants as fetched now.
	MetaFetched bool
	// AboutFetched marks the about field as fetched now (also when about is empty).
	AboutFetched bool
}

type CatalogSort string

const (
	SortRecent CatalogSort = "recent"
	SortSize   CatalogSort = "size"
	SortAlpha  CatalogSort = "alpha"
)

func ParseCatalogSort(raw string) CatalogSort {
	switch CatalogSort(raw) {
	case SortSize:
		return SortSize
	case SortAlpha:
		return SortAlpha
	default:
		return SortRecent
	}
}

func (s CatalogSort) Next() CatalogSort {
	switch s {
	case SortRecent:
		return SortSize
	case SortSize:
		return SortAlpha
	default:
		return SortRecent
	}
}

type ChatCatalogQuery struct {
	db *dbutil.Database
}

const (
	chatCatalogColumns = `user_id, portal_id, kind, title, about, username, participants,
		is_forum, is_broadcast, in_archive, meta_fetched_ts, about_fetched_ts, search_blob`

	getChatCatalogRowQuery = "SELECT " + chatCatalogColumns + " FROM chat_catalog WHERE user_id=$1 AND portal_id=$2"
	listChatCatalogQuery   = "SELECT " + chatCatalogColumns + " FROM chat_catalog WHERE user_id=$1"
	insertChatCatalogQuery = `
		INSERT INTO chat_catalog (` + chatCatalogColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (user_id, portal_id) DO UPDATE SET
			kind=$3, title=$4, about=$5, username=$6, participants=$7, is_forum=$8,
			is_broadcast=$9, in_archive=$10, meta_fetched_ts=$11, about_fetched_ts=$12,
			search_blob=$13
	`
	deleteChatCatalogQuery = "DELETE FROM chat_catalog WHERE user_id=$1 AND portal_id=$2"
)

func scanChatCatalogRow(row dbutil.Scannable) (*ChatCatalogRow, error) {
	var r ChatCatalogRow
	var isForum, isBroadcast, inArchive int
	err := row.Scan(
		&r.UserID, &r.PortalID, &r.Kind, &r.Title, &r.About, &r.Username, &r.Participants,
		&isForum, &isBroadcast, &inArchive, &r.MetaFetchedTS, &r.AboutFetchedTS, &r.SearchBlob,
	)
	if err != nil {
		return nil, err
	}
	r.IsForum = isForum != 0
	r.IsBroadcast = isBroadcast != 0
	r.InArchive = inArchive != 0
	return &r, nil
}

func (r *ChatCatalogRow) sqlVariables() []any {
	return []any{
		r.UserID, r.PortalID, r.Kind, r.Title, r.About, r.Username, r.Participants,
		boolToInt(r.IsForum), boolToInt(r.IsBroadcast), boolToInt(r.InArchive),
		r.MetaFetchedTS, r.AboutFetchedTS, r.SearchBlob,
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// NormalizeSearch lowercases the input, replaces ё with е and collapses
// whitespace. It is used both when writing search_blob and when matching.
func NormalizeSearch(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	return strings.Join(strings.Fields(s), " ")
}

// BuildCatalogSearchBlob builds the normalized searchable text of a catalog row.
func BuildCatalogSearchBlob(r *ChatCatalogRow) string {
	var b strings.Builder
	appendField := func(v string) {
		if v == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(v)
	}
	appendField(NormalizeSearch(r.Title))
	appendField(NormalizeSearch(strings.TrimPrefix(r.Username, "@")))
	appendField(NormalizeSearch(r.About))
	appendField(NormalizeSearch(r.PortalID))
	if _, id, ok := strings.Cut(r.PortalID, ":"); ok {
		appendField(id)
	}
	if r.Participants > 0 {
		appendField(strconv.Itoa(r.Participants))
	}
	return b.String()
}

func (q *ChatCatalogQuery) Get(ctx context.Context, userID int64, portalID string) (*ChatCatalogRow, error) {
	rows, err := dbutil.ConvertRowFn[*ChatCatalogRow](scanChatCatalogRow).
		NewRowIter(q.db.Query(ctx, getChatCatalogRowQuery, userID, portalID)).AsList()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}

// Update applies a patch to a catalog row, creating it when it doesn't exist.
func (q *ChatCatalogQuery) Update(ctx context.Context, userID int64, portalID string, patch CatalogPatch) error {
	row, err := q.Get(ctx, userID, portalID)
	if errors.Is(err, sql.ErrNoRows) {
		row = &ChatCatalogRow{UserID: userID, PortalID: portalID}
	} else if err != nil {
		return err
	}
	now := time.Now().Unix()
	if patch.Kind != nil {
		row.Kind = *patch.Kind
	}
	if patch.Title != nil {
		row.Title = *patch.Title
	}
	if patch.About != nil {
		row.About = *patch.About
	}
	if patch.Username != nil {
		row.Username = *patch.Username
	}
	if patch.Participants != nil {
		row.Participants = *patch.Participants
	}
	if patch.IsForum != nil {
		row.IsForum = *patch.IsForum
	}
	if patch.IsBroadcast != nil {
		row.IsBroadcast = *patch.IsBroadcast
	}
	if patch.InArchive != nil {
		row.InArchive = *patch.InArchive
	}
	if patch.MetaFetched || row.MetaFetchedTS == 0 {
		row.MetaFetchedTS = now
	}
	if patch.AboutFetched {
		row.AboutFetchedTS = now
	}
	row.SearchBlob = BuildCatalogSearchBlob(row)
	_, err = q.db.Exec(ctx, insertChatCatalogQuery, row.sqlVariables()...)
	return err
}

func (q *ChatCatalogQuery) Delete(ctx context.Context, userID int64, portalID string) error {
	_, err := q.db.Exec(ctx, deleteChatCatalogQuery, userID, portalID)
	return err
}

// List returns every catalog row of the login. Filtering and ranking are done
// in Go: the table is small and strings.Contains handles unicode without any
// SQL LIKE quirks.
func (q *ChatCatalogQuery) List(ctx context.Context, userID int64) ([]*ChatCatalogRow, error) {
	return dbutil.ConvertRowFn[*ChatCatalogRow](scanChatCatalogRow).
		NewRowIter(q.db.Query(ctx, listChatCatalogQuery, userID)).AsList()
}

// Search filters the catalog by the normalized query and sorts the result by
// match quality. An empty query returns everything sorted by the given order.
func (q *ChatCatalogQuery) Search(
	ctx context.Context, userID int64, query string, order CatalogSort, limit, offset int,
) ([]*ChatCatalogRow, int, error) {
	all, err := q.List(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	query = NormalizeSearch(strings.TrimPrefix(query, "@"))
	type scoredRow struct {
		row   *ChatCatalogRow
		score int
	}
	matches := make([]scoredRow, 0, len(all))
	for _, row := range all {
		score := ScoreCatalogRow(row, query)
		if score <= 0 {
			continue
		}
		matches = append(matches, scoredRow{row, score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if query != "" && matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return CompareCatalogRows(matches[i].row, matches[j].row, order)
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
	rows := make([]*ChatCatalogRow, len(matches))
	for i, match := range matches {
		rows[i] = match.row
	}
	return rows, total, nil
}

// ScoreCatalogRow returns the match quality of a row for a normalized query.
// 0 means no match.
func ScoreCatalogRow(row *ChatCatalogRow, query string) int {
	if query == "" {
		return 1
	}
	title := NormalizeSearch(row.Title)
	about := NormalizeSearch(row.About)
	username := NormalizeSearch(strings.TrimPrefix(row.Username, "@"))
	portalID := NormalizeSearch(row.PortalID)
	_, numericID, _ := strings.Cut(row.PortalID, ":")
	participants := ""
	if row.Participants > 0 {
		participants = strconv.Itoa(row.Participants)
	}
	switch {
	case portalID == query || numericID == query:
		return 100
	case username != "" && username == query:
		return 90
	case strings.HasPrefix(username, query):
		return 80
	case title != "" && title == query:
		return 70
	case strings.HasPrefix(title, query):
		return 60
	case strings.Contains(title, query):
		return 50
	case strings.Contains(about, query):
		return 40
	case participants != "" && strings.HasPrefix(participants, query):
		return 30
	case strings.Contains(portalID, query), numericID != "" && strings.Contains(numericID, query):
		return 20
	case strings.Contains(row.SearchBlob, query):
		return 10
	}
	return 0
}

// CompareCatalogRows orders rows by the given sort mode.
func CompareCatalogRows(a, b *ChatCatalogRow, order CatalogSort) bool {
	switch order {
	case SortSize:
		if a.Participants != b.Participants {
			return a.Participants > b.Participants
		}
	case SortAlpha:
		if !strings.EqualFold(a.Title, b.Title) {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
	}
	return a.Participants > b.Participants
}
