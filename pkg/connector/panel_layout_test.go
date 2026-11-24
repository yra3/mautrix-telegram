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
	"testing"

	"go.mau.fi/mautrix-telegram/pkg/connector/panel"
)

func TestPanelLayoutRoundTrip(t *testing.T) {
	rows := []panel.Row{
		{PortalID: "channel:1", Title: "A"},
		{PortalID: "channel:2"},
	}
	raw := buildPanelLayout(2, 10, rows)
	layout, ok := parsePanelLayout(raw)
	if !ok {
		t.Fatalf("parsePanelLayout(%q) failed", raw)
	}
	if layout.Page != 2 {
		t.Errorf("page = %d, want 2", layout.Page)
	}
	if len(layout.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(layout.Rows))
	}
	if layout.Rows[0].Number != 11 || layout.Rows[0].PortalID != "channel:1" || layout.Rows[0].Title != "A" {
		t.Errorf("row 0 = %+v", layout.Rows[0])
	}
	if layout.Rows[1].Number != 12 || layout.Rows[1].PortalID != "channel:2" {
		t.Errorf("row 1 = %+v", layout.Rows[1])
	}
}

func TestPanelLayoutRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "{", "{}", `{"page":0,"rows":[]}`} {
		if _, ok := parsePanelLayout(raw); ok {
			t.Errorf("parsePanelLayout(%q) accepted an unusable layout", raw)
		}
	}
}
