package panel

import (
	"strings"
	"testing"
)

func TestTotalPages(t *testing.T) {
	cases := []struct{ total, want int }{
		{0, 1}, {1, 1}, {5, 1}, {6, 2}, {11, 3},
	}
	for _, c := range cases {
		if got := TotalPages(c.total); got != c.want {
			t.Errorf("TotalPages(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}

func TestClampPage(t *testing.T) {
	if got := ClampPage(-1, 11); got != 0 {
		t.Errorf("ClampPage(-1, 11) = %d, want 0", got)
	}
	if got := ClampPage(5, 11); got != 2 {
		t.Errorf("ClampPage(5, 11) = %d, want 2", got)
	}
	if got := ClampPage(1, 11); got != 1 {
		t.Errorf("ClampPage(1, 11) = %d, want 1", got)
	}
	if got := ClampPage(3, 0); got != 0 {
		t.Errorf("ClampPage(3, 0) = %d, want 0", got)
	}
}

func TestParseKey(t *testing.T) {
	cases := []struct {
		key  string
		kind ActionKind
		row  int
	}{
		{"1️⃣", ActionRow, 0},
		{"2\ufe0f\u20e3", ActionRow, 1},
		{"5️⃣", ActionRow, 4},
		{"①", ActionRow, 0},
		{"⑤", ActionRow, 4},
		{"3", ActionRow, 2},
		{"⬅️", ActionPrev, 0},
		{"➡", ActionNext, 0},
		{"🔍", ActionSearch, 0},
		{"🔄", ActionRefresh, 0},
		{"🔀", ActionSort, 0},
	}
	for _, c := range cases {
		action, ok := ParseKey(c.key)
		if !ok {
			t.Errorf("ParseKey(%q) did not match", c.key)
			continue
		}
		if action.Kind != c.kind || action.Row != c.row {
			t.Errorf("ParseKey(%q) = %+v, want kind %v row %d", c.key, action, c.kind, c.row)
		}
	}
	if _, ok := ParseKey("🎉"); ok {
		t.Errorf("ParseKey accepted an unknown emoji")
	}
}

func TestDesiredKeys(t *testing.T) {
	keys := DesiredKeys(2)
	if len(keys) != 7 {
		t.Fatalf("DesiredKeys(2) length = %d, want 7", len(keys))
	}
	// Page with more rows than emojis must cap at five buttons.
	if len(DesiredKeys(10)) != 10 {
		t.Errorf("DesiredKeys(10) must cap row buttons at 5")
	}
}

func TestRenderFiveRowsPerPageAndContinuousNumbering(t *testing.T) {
	page := Page{
		UserLabel: "@yura (1)",
		Sort:      "recent",
		PageIndex: 1,
		TotalRows: 11,
		Offset:    5,
		Rows: []Row{
			{PortalID: "channel:1", Title: "A", Reason: "too many participants", LastSeenTS: 1700000000},
			{PortalID: "channel:2", Title: "B", HasMXID: true, RoomID: "!room:example.org"},
		},
		Lang: "ru",
	}
	out := Render(page)
	if !strings.Contains(out, "Стр. 2/3") {
		t.Errorf("missing page counter in %q", out)
	}
	// The first row of page 2 is number 6.
	if !strings.Contains(out, "① 6. «A»") {
		t.Errorf("missing the row button badge in\n%s", out)
	}
	if !strings.Contains(out, "② 7. «B»") {
		t.Errorf("numbering must continue across pages:\n%s", out)
	}
	if !strings.Contains(out, "`channel:1`") {
		t.Errorf("row id missing:\n%s", out)
	}
	if !strings.Contains(out, "🔀") {
		t.Errorf("footer missing:\n%s", out)
	}
}

func TestRenderEmpty(t *testing.T) {
	out := Render(Page{TotalRows: 0, Lang: "en"})
	if !strings.Contains(out, "Nothing found") {
		t.Errorf("empty state missing:\n%s", out)
	}
}
