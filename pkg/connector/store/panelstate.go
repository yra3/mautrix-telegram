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
	"time"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/id"
)

// PanelState is the state of the single live declined-chats panel of a user.
type PanelState struct {
	UserMXID    id.UserID
	RoomID      id.RoomID
	EventID     id.EventID
	Page        int
	FilterQuery string
	Sort        string
	Generation  int64
	CreatedTS   int64
	UpdatedTS   int64
	// Layout is the JSON of the rendered page: the chat behind every row button.
	Layout string
}

type PanelStateQuery struct {
	db *dbutil.Database
}

const (
	panelStateColumns = "user_mxid, room_id, event_id, page, filter_query, sort, generation, created_ts, updated_ts, layout"

	getPanelStateQuery    = "SELECT " + panelStateColumns + " FROM declined_panel WHERE user_mxid=$1"
	upsertPanelStateQuery = `
		INSERT INTO declined_panel (` + panelStateColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_mxid) DO UPDATE SET
			room_id=$2, event_id=$3, page=$4, filter_query=$5, sort=$6,
			generation=$7, created_ts=$8, updated_ts=$9, layout=$10
	`
	deletePanelStateQuery = "DELETE FROM declined_panel WHERE user_mxid=$1"
)

func scanPanelState(row dbutil.Scannable) (*PanelState, error) {
	var p PanelState
	err := row.Scan(
		&p.UserMXID, &p.RoomID, &p.EventID, &p.Page, &p.FilterQuery, &p.Sort,
		&p.Generation, &p.CreatedTS, &p.UpdatedTS, &p.Layout,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *PanelState) sqlVariables() []any {
	return []any{
		p.UserMXID, p.RoomID, p.EventID, p.Page, p.FilterQuery, p.Sort,
		p.Generation, p.CreatedTS, p.UpdatedTS, p.Layout,
	}
}

func (q *PanelStateQuery) Get(ctx context.Context, userMXID id.UserID) (*PanelState, error) {
	rows, err := dbutil.ConvertRowFn[*PanelState](scanPanelState).
		NewRowIter(q.db.Query(ctx, getPanelStateQuery, userMXID)).AsList()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}

// Save writes the panel state and bumps the generation counter.
func (q *PanelStateQuery) Save(ctx context.Context, state *PanelState) error {
	now := time.Now().Unix()
	if state.CreatedTS == 0 {
		state.CreatedTS = now
	}
	state.UpdatedTS = now
	state.Generation++
	_, err := q.db.Exec(ctx, upsertPanelStateQuery, state.sqlVariables()...)
	return err
}

func (q *PanelStateQuery) Delete(ctx context.Context, userMXID id.UserID) error {
	_, err := q.db.Exec(ctx, deletePanelStateQuery, userMXID)
	return err
}

// GetOrCreate returns the stored panel state or a new one bound to the room.
func (q *PanelStateQuery) GetOrCreate(ctx context.Context, userMXID id.UserID, roomID id.RoomID) (*PanelState, error) {
	state, err := q.Get(ctx, userMXID)
	if errors.Is(err, sql.ErrNoRows) {
		return &PanelState{UserMXID: userMXID, RoomID: roomID, Sort: string(SortRecent)}, nil
	}
	return state, err
}
