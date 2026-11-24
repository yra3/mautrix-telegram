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

// Package panel contains the pure presentation logic of the declined-chats
// panel: pagination, rendering and reaction key mapping. All Matrix and
// Telegram side effects live in the connector package.
package panel

import (
	"fmt"
	"strings"
	"time"

	"go.mau.fi/mautrix-telegram/pkg/connector/store"
)

const PageSize = 5

const (
	KeyPrev    = "⬅️"
	KeyNext    = "➡️"
	KeySearch  = "🔍"
	KeyRefresh = "🔄"
	KeySort    = "🔀"
)

// RowEmojis are the per-row buttons, one per visible row. Circled digits are
// single codepoints: keycap sequences (1️⃣) render as empty boxes in
// clients whose font has no keycap glyph, e.g. Lightning.
var RowEmojis = []string{"①", "②", "③", "④", "⑤"}

// Row is one line of the panel.
type Row struct {
	PortalID     string
	Title        string
	Username     string
	Participants int
	Reason       string
	LastSeenTS   int64
	HasMXID      bool
	RoomID       string
	Rejected     bool
	InArchive    bool
	Kind         string
}

// Page is everything needed to render one panel message.
type Page struct {
	// UserLabel is the Matrix user plus Telegram id, e.g. "@yura (5938699949)".
	UserLabel string
	Query     string
	Sort      store.CatalogSort
	PageIndex int
	TotalRows int
	Offset    int
	Rows      []Row
	Status    string
	Lang      string
}

type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionRow
	ActionPrev
	ActionNext
	ActionSearch
	ActionRefresh
	ActionSort
)

type Action struct {
	Kind ActionKind
	// Row is the 0-based index inside the currently visible page.
	Row int
}

// circledDigits maps the row buttons to plain digits.
var circledDigits = map[string]string{
	"①": "1", "②": "2", "③": "3", "④": "4", "⑤": "5",
	"⑥": "6", "⑦": "7", "⑧": "8", "⑨": "9",
}

// NormalizeKey folds every way a row button can arrive into a plain digit:
// keycap sequences ("1️⃣" from older panels, "1⃣" from clients that drop
// the variation selector), circled digits, and bare digits.
func NormalizeKey(raw string) string {
	key := strings.ReplaceAll(strings.TrimSpace(raw), "\ufe0f", "")
	key = strings.ReplaceAll(key, "\u20e3", "")
	if digit, ok := circledDigits[key]; ok {
		return digit
	}
	return key
}

// ParseKey maps a reaction key to an action.
func ParseKey(raw string) (Action, bool) {
	switch NormalizeKey(raw) {
	case NormalizeKey(KeyPrev):
		return Action{Kind: ActionPrev}, true
	case NormalizeKey(KeyNext):
		return Action{Kind: ActionNext}, true
	case NormalizeKey(KeySearch):
		return Action{Kind: ActionSearch}, true
	case NormalizeKey(KeyRefresh):
		return Action{Kind: ActionRefresh}, true
	case NormalizeKey(KeySort):
		return Action{Kind: ActionSort}, true
	}
	normalized := NormalizeKey(raw)
	for i, emoji := range RowEmojis {
		if NormalizeKey(emoji) == normalized {
			return Action{Kind: ActionRow, Row: i}, true
		}
	}
	return Action{}, false
}

// TotalPages returns the amount of pages for total rows (at least one).
func TotalPages(total int) int {
	if total <= 0 {
		return 1
	}
	return (total + PageSize - 1) / PageSize
}

// ClampPage keeps the page index inside the valid range.
func ClampPage(page, total int) int {
	last := TotalPages(total) - 1
	if page < 0 {
		return 0
	}
	if page > last {
		return last
	}
	return page
}

// DesiredKeys is the full set of reaction buttons for a rendered page.
func DesiredKeys(rowCount int) []string {
	if rowCount > len(RowEmojis) {
		rowCount = len(RowEmojis)
	}
	keys := make([]string, 0, rowCount+5)
	keys = append(keys, RowEmojis[:rowCount]...)
	return append(keys, KeyPrev, KeyNext, KeySearch, KeyRefresh, KeySort)
}

func sortLabel(sort store.CatalogSort, ru bool) string {
	switch sort {
	case store.SortSize:
		if ru {
			return "по размеру"
		}
		return "by size"
	case store.SortAlpha:
		if ru {
			return "по алфавиту"
		}
		return "alphabetical"
	default:
		if ru {
			return "по дате"
		}
		return "most recent"
	}
}

func cleanTitle(title string) string {
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.TrimSpace(title)
	if title == "" {
		return "(без названия)"
	}
	return title
}

// Render builds the markdown body of the panel message.
func Render(p Page) string {
	ru := p.Lang == "ru"
	var b strings.Builder

	if ru {
		b.WriteString("**Отклонённые чаты**")
	} else {
		b.WriteString("**Declined chats**")
	}
	if p.UserLabel != "" {
		b.WriteString(" — ")
		b.WriteString(p.UserLabel)
	}
	b.WriteString("\n")

	totalPages := TotalPages(p.TotalRows)
	fmt.Fprintf(&b, "%s %d/%d", tr(ru, "Page", "Стр."), p.PageIndex+1, totalPages)
	if p.Query != "" {
		fmt.Fprintf(&b, " · %s «%s» %s", tr(ru, "filter:", "фильтр:"), p.Query, fmt.Sprintf(tr(ru, "%d found", "%d найдено"), p.TotalRows))
	} else {
		fmt.Fprintf(&b, " · %s %d", tr(ru, "total:", "всего:"), p.TotalRows)
	}
	fmt.Fprintf(&b, " · %s: %s\n", tr(ru, "sort", "сортировка"), sortLabel(p.Sort, ru))

	if p.Status != "" {
		b.WriteString("\n")
		b.WriteString(p.Status)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(p.Rows) == 0 {
		b.WriteString(tr(ru, "_Nothing found._", "_Ничего не найдено._"))
		b.WriteString("\n")
	}

	for i, row := range p.Rows {
		num := p.Offset + i + 1
		if i < len(RowEmojis) {
			// The badge is the button that acts on this row on the current page.
			fmt.Fprintf(&b, "%s %d. «%s» · `%s`", RowEmojis[i], num, cleanTitle(row.Title), row.PortalID)
		} else {
			fmt.Fprintf(&b, "%d. «%s» · `%s`", num, cleanTitle(row.Title), row.PortalID)
		}
		if row.Participants > 0 {
			fmt.Fprintf(&b, " · %d %s", row.Participants, tr(ru, "members", "уч."))
		}
		if row.Username != "" {
			fmt.Fprintf(&b, " · @%s", strings.TrimPrefix(row.Username, "@"))
		}
		b.WriteString("\n")

		details := make([]string, 0, 3)
		if row.Rejected && row.Reason != "" {
			details = append(details, "_"+cleanTitle(row.Reason)+"_")
		} else if !row.Rejected {
			details = append(details, tr(ru, "_not declined_", "_не отклонён_"))
		}
		if row.LastSeenTS > 0 {
			details = append(details, time.Unix(row.LastSeenTS, 0).Format("02.01 15:04"))
		}
		switch {
		case row.RoomID != "":
			details = append(details, tr(ru, "room ", "комната ")+"`"+row.RoomID+"`")
		case row.HasMXID:
			details = append(details, tr(ru, "room exists", "комната есть"))
		case row.InArchive:
			details = append(details, tr(ru, "archived in Matrix", "в архиве Matrix"))
		default:
			details = append(details, tr(ru, "no room", "комнаты нет"))
		}
		fmt.Fprintf(&b, "   %s\n", strings.Join(details, " · "))
	}

	b.WriteString("\n")
	if ru {
		b.WriteString("⬅️/➡️ — страницы · ①–⑤ — действие со строкой\n")
		if p.Query != "" {
			b.WriteString("🔍 сбросить фильтр · 🔄 обновить · 🔀 сортировка")
		} else {
			b.WriteString("🔍 поиск: `!s <запрос>` · 🔄 обновить · 🔀 сортировка")
		}
	} else {
		b.WriteString("⬅️/➡️ — pages · ①–⑤ — act on a row\n")
		if p.Query != "" {
			b.WriteString("🔍 clear the filter · 🔄 refresh · 🔀 sort")
		} else {
			b.WriteString("🔍 search: `!s <query>` · 🔄 refresh · 🔀 sort")
		}
	}
	return b.String()
}

func tr(ru bool, en, russian string) string {
	if ru {
		return russian
	}
	return en
}
