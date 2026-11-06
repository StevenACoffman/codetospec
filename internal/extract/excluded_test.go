package extract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// TestWithoutExcluded: a fact that describes the code it cites is dropped
// when that code is excluded; other kinds, and citations of another root,
// stay.
func TestWithoutExcluded(t *testing.T) {
	excluded := func(path string) bool { return strings.HasPrefix(path, "testutil/") }
	at := func(id, kind, path, root string) extract.Fact {
		return extract.Fact{
			ID: id, Kind: kind,
			Source: extract.Ref{Path: path, Lines: "1-2", Root: root},
		}
	}
	facts := []extract.Fact{
		at("rule.kept", extract.KindRule, "roles/dev.go", ""),
		at("rule.leaked", extract.KindRule, "testutil/suite.go", ""),
		at("domain.leaked", extract.KindDomain, "testutil/suite.go", ""),
		at("rule.inherited", extract.KindRule, "testutil/shared.go", "pkg"),
		at("test.evidence", extract.KindTest, "testutil/suite.go", ""),
		at("call.kept", extract.KindCall, "testutil/suite.go", ""),
		at("access_check.leaked", extract.KindAccessCheck, "testutil/checks.go", ""),
		at("access_check.uncited", extract.KindAccessCheck, "", ""), // known by name only
	}

	kept, dropped := extract.WithoutExcluded(facts, excluded)

	var keptIDs []string
	for _, f := range kept {
		keptIDs = append(keptIDs, f.ID)
	}
	if want := []string{
		"rule.kept",
		"rule.inherited",
		"test.evidence",
		"call.kept",
		"access_check.uncited",
	}; !slices.Equal(
		keptIDs,
		want,
	) {
		t.Errorf("kept = %v, want %v", keptIDs, want)
	}
	if want := []string{
		"access_check.leaked",
		"domain.leaked",
		"rule.leaked",
	}; !slices.Equal(
		dropped,
		want,
	) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
}
