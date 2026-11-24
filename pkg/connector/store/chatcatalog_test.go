package store

import (
	"strings"
	"testing"
)

func TestNormalizeSearch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Ёлка", "елка"},
		{"  Семья   Друзей  ", "семья друзей"},
		{"МОСКВА-ЁЖ", "москва-еж"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeSearch(c.in); got != c.want {
			t.Errorf("NormalizeSearch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildCatalogSearchBlob(t *testing.T) {
	row := &ChatCatalogRow{
		PortalID:     "channel:2463355752",
		Title:        "Семья Ёлкина",
		Username:     "@family",
		About:        "Домашний чат",
		Participants: 23,
	}
	blob := BuildCatalogSearchBlob(row)
	for _, want := range []string{"семья елкина", "family", "домашний чат", "channel:2463355752", "2463355752", "23"} {
		if !strings.Contains(blob, want) {
			t.Errorf("blob %q does not contain %q", blob, want)
		}
	}
}

func TestScoreCatalogRowOrdering(t *testing.T) {
	row := &ChatCatalogRow{
		PortalID:     "channel:2463355752",
		Title:        "Семья",
		Username:     "@family_chat",
		About:        "Обсуждение семейных дел",
		Participants: 23,
		SearchBlob:   "семья family_chat обсуждение семейных дел channel:2463355752 2463355752 23",
	}
	// Exact id beats everything.
	if got := ScoreCatalogRow(row, "channel:2463355752"); got != 100 {
		t.Errorf("exact id score = %d, want 100", got)
	}
	if got := ScoreCatalogRow(row, "2463355752"); got != 100 {
		t.Errorf("numeric id score = %d, want 100", got)
	}
	if got := ScoreCatalogRow(row, "family_chat"); got != 90 {
		t.Errorf("username score = %d, want 90", got)
	}
	if got := ScoreCatalogRow(row, "семья"); got != 70 {
		t.Errorf("exact title score = %d, want 70", got)
	}
	if got := ScoreCatalogRow(row, "сем"); got != 60 {
		t.Errorf("title prefix score = %d, want 60", got)
	}
	if got := ScoreCatalogRow(row, "обсуждение"); got != 40 {
		t.Errorf("about score = %d, want 40", got)
	}
	if got := ScoreCatalogRow(row, "ничего"); got != 0 {
		t.Errorf("no match score = %d, want 0", got)
	}
}

func TestScoreCatalogRowCaseAndYoInsensitive(t *testing.T) {
	row := &ChatCatalogRow{
		PortalID:   "channel:1",
		Title:      "Ёжики и Семья",
		SearchBlob: "ежики и семья channel:1 1",
	}
	if got := ScoreCatalogRow(row, NormalizeSearch("ёжики")); got == 0 {
		t.Errorf("ё-insensitive search did not match")
	}
	if got := ScoreCatalogRow(row, NormalizeSearch("СЕМЬЯ")); got == 0 {
		t.Errorf("case-insensitive search did not match")
	}
}

func TestScoreRejectionRow(t *testing.T) {
	row := &ChatRejectionRow{
		ID:         "channel:42",
		Title:      "Семья",
		Reason:     "too many participants",
		Username:   "family",
		Source:     RejectionSourceAuto,
		HasMXID:    false,
		LastSeenTS: 0,
	}
	// The reason is searchable.
	if got := ScoreRejectionRow(row, "participants"); got != 50 {
		t.Errorf("reason score = %d, want 50", got)
	}
	if got := ScoreRejectionRow(row, "нет-такого"); got != 0 {
		t.Errorf("no match score = %d, want 0", got)
	}
}

func TestCatalogSortCycle(t *testing.T) {
	if ParseCatalogSort("").Next() != SortSize {
		t.Errorf("recent -> size cycle broken")
	}
	if SortSize.Next() != SortAlpha {
		t.Errorf("size -> alpha cycle broken")
	}
	if SortAlpha.Next() != SortRecent {
		t.Errorf("alpha -> recent cycle broken")
	}
	if ParseCatalogSort("garbage") != SortRecent {
		t.Errorf("unknown sort must fall back to recent")
	}
}
