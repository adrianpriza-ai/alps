package more

import (
	"strings"
	"testing"
)

// TestEntryMatchesQuery verifies which entry fields participate in a search:
// name, description, author, and dependencies, all case-insensitive.
func TestEntryMatchesQuery(t *testing.T) {
	e := &Entry{
		Name:   "mytool",
		Desc:   "a helpful utility",
		Author: "Jane Doe",
		Deps:   []string{"python3", "libssl-dev"},
	}

	tests := []struct {
		query string
		want  bool
	}{
		{"mytool", true},  // name
		{"MYTOOL", true},  // case-insensitive name
		{"helpful", true}, // desc
		{"jane", true},    // author
		{"doe", true},     // author, case-insensitive
		{"python3", true}, // dependency
		{"libssl", true},  // partial dependency match
		{"other", false},  // no match anywhere
	}
	for _, tc := range tests {
		if got := entryMatchesQuery(e, strings.ToLower(tc.query)); got != tc.want {
			t.Errorf("entryMatchesQuery(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}

	// The empty query matches everything.
	if !entryMatchesQuery(e, "") {
		t.Error("empty query should match any entry")
	}
}

// TestEntryMatchesQuerySparse verifies that an entry without author or deps
// still matches on name and description.
func TestEntryMatchesQuerySparse(t *testing.T) {
	e := &Entry{Name: "bare", Desc: "minimal package"}
	if !entryMatchesQuery(e, "bare") {
		t.Error("name match should work for an entry without author/deps")
	}
	if !entryMatchesQuery(e, "minimal") {
		t.Error("desc match should work for an entry without author/deps")
	}
	if entryMatchesQuery(e, "nobody") {
		t.Error("should not match when author is empty")
	}
}
