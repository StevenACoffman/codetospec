package extract_test

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// The fake extractor's name and its behaviour flag.
const (
	echoName = "echo"
	modeFlag = "--mode"
)

// echoExtractor builds the fake extractor and returns its path. It is built
// before the extractor runs, so the timeout tests time the run alone.
func echoExtractor(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "echoextractor")
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "./testdata/echoextractor").
		CombinedOutput()
	if err != nil {
		t.Fatalf("build echoextractor: %v\n%s", err, out)
	}
	return bin
}

func TestRunExtractorSuccess(t *testing.T) {
	facts, err := extract.RunExtractor(
		context.Background(),
		slog.New(slog.NewTextHandler(t.Output(), nil)),
		extract.ExtractorConfig{
			Name: echoName,
			Cmd:  "go",
			Args: []string{
				"run",
				"./testdata/echoextractor",
				modeFlag,
				"ok",
				"--root",
				"{src}",
				"--lang",
				"{lang}",
			},
			Timeout: 120 * time.Second,
		},
		"/repo/src",
		"fr",
	)
	if err != nil {
		t.Fatalf("RunExtractor: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(facts))
	}
	f := facts[0]
	if f.Origin != echoName {
		t.Errorf("origin = %q, want echo (forced to extractor name)", f.Origin)
	}
	if f.Attrs["root"] != "/repo/src" {
		t.Errorf("{src} not substituted, got %q", f.Attrs["root"])
	}
	if f.Attrs["lang"] != "fr" {
		t.Errorf("{lang} not substituted, got %q", f.Attrs["lang"])
	}
	if f.Certainty != extract.CertaintyProved {
		t.Errorf("certainty = %q, want proved", f.Certainty)
	}
}

func TestRunExtractorTimeout(t *testing.T) {
	_, err := extract.RunExtractor(
		context.Background(),
		slog.New(slog.NewTextHandler(t.Output(), nil)),
		extract.ExtractorConfig{
			Name:    echoName,
			Cmd:     echoExtractor(t),
			Args:    []string{modeFlag, "hang"},
			Timeout: 500 * time.Millisecond,
		},
		"/repo/src",
		"fr",
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestRunExtractorNonZeroExit(t *testing.T) {
	_, err := extract.RunExtractor(
		context.Background(),
		slog.New(slog.NewTextHandler(t.Output(), nil)),
		extract.ExtractorConfig{
			Name: echoName,
			Cmd:  echoExtractor(t),
			Args: []string{modeFlag, "fail"},
		},
		"/repo/src",
		"fr",
	)
	if err == nil {
		t.Fatal("want error on non-zero exit")
	}
}

func TestRunExtractorInvalidJSON(t *testing.T) {
	_, err := extract.RunExtractor(
		context.Background(),
		slog.New(slog.NewTextHandler(t.Output(), nil)),
		extract.ExtractorConfig{
			Name: echoName,
			Cmd:  echoExtractor(t),
			Args: []string{modeFlag, "garbage"},
		},
		"/repo/src",
		"fr",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid facts JSON") {
		t.Fatalf("want invalid JSON error, got %v", err)
	}
}

func TestLoadFactsFile(t *testing.T) {
	facts, err := extract.LoadFactsFile(
		filepath.Join("..", "..", "testdata", "fixture", "fixture.facts.json"),
	)
	if err != nil {
		t.Fatalf("LoadFactsFile: %v", err)
	}
	if len(facts) != 3 {
		t.Fatalf("facts = %d, want 3", len(facts))
	}
	kinds := map[string]int{}
	for _, f := range facts {
		kinds[f.Kind]++
		if f.Origin == "" {
			t.Errorf("fact %s has empty origin", f.ID)
		}
	}
	if kinds["route"] != 2 || kinds["table"] != 1 {
		t.Fatalf("kinds = %v, want 2 routes and 1 table", kinds)
	}
}
