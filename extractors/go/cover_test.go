package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// coveredFile is the one file of the covered module.
const coveredFile = "pay.go"

// TestExecutedFacts: a profile becomes the executed and unexecuted lines
// of each module file it names; a file of another module is ignored, and
// one changed since the profile is left out.
func TestExecutedFacts(t *testing.T) {
	m, dir := loadModule(t, "example.com/pay", map[string]string{
		coveredFile: `package pay

func Pay(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
`,
	})
	profile := filepath.Join(t.TempDir(), "cover.out")
	writeFile(t, profile, `mode: set
example.com/pay/pay.go:3.21,4.11 1 1
example.com/pay/pay.go:4.11,6.3 1 0
example.com/pay/pay.go:7.2,7.10 1 1
example.org/other/x.go:1.1,2.2 1 1
`)
	profiles, stale, err := readProfile(profile, dir)
	if err != nil {
		t.Fatal(err)
	}

	facts, skipped := ExecutedFacts(profiles, profileFiles(m.pkgs, dir), stale)
	if len(facts) != 1 || len(skipped) != 0 {
		t.Fatalf("facts %+v, skipped %v: want one fact", facts, skipped)
	}
	f := facts[0]
	if f.ID != "executed."+coveredFile || f.Source.Lines != "1-7" ||
		f.Attrs[attrExecuted] != "3-4,7-7" || f.Attrs[attrUnexecuted] != "5-6" {
		t.Errorf("fact = %+v", f)
	}

	later := time.Now().Add(time.Hour)
	if err = os.Chtimes(filepath.Join(dir, coveredFile), later, later); err != nil {
		t.Fatal(err)
	}
	facts, skipped = ExecutedFacts(profiles, profileFiles(m.pkgs, dir), stale)
	if len(facts) != 0 || !slices.Equal(skipped, []string{coveredFile}) {
		t.Errorf("after an edit: facts %+v, skipped %v", facts, skipped)
	}
}

// writeFile writes content to path.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
