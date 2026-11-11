package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/anarch"
	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/extract"
)

// export --format anarch writes the writers rules the run's facts propose,
// leaving out the rules --anarch's model declares, into a new directory.
func TestExportAnarch(t *testing.T) {
	out := t.TempDir()
	stored := func(kind, file, field string) []extract.Fact {
		return []extract.Fact{
			{
				Kind: extract.KindTable, ID: "table.webapp." + kind,
				Attrs: map[string]string{extract.AttrName: kind}, Source: extract.Ref{Path: file},
			},
			{
				Kind: extract.KindWrites, ID: "writes.emails/send.go:1-9#" + field,
				Attrs: map[string]string{
					extract.AttrField: field, extract.AttrFieldPackage: "example.com/svc/models",
				},
				Source: extract.Ref{Path: "emails/send.go", Lines: "1-9"},
			},
		}
	}
	facts := []extract.Fact{
		{
			Kind: extract.KindPackage, ID: "package.example.com/svc/models",
			Attrs:  map[string]string{extract.AttrName: "example.com/svc/models"},
			Source: extract.Ref{Path: "models/run.go"},
		},
		{
			Kind: extract.KindPackage, ID: "package.example.com/svc/emails",
			Attrs:  map[string]string{extract.AttrName: "example.com/svc/emails"},
			Source: extract.Ref{Path: "emails/send.go"},
		},
	}
	facts = append(facts, stored("Run", "models/run.go", "models.Run.Status")...)
	facts = append(facts, stored("Job", "models/job.go", "models.Job.State")...)
	envelope, err := json.Marshal(map[string]any{"schema": extract.FactsSchema, "facts": facts})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(out, ".codetospec"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(
		filepath.Join(out, ".codetospec", "facts.json"),
		envelope,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(t.TempDir(), "anarch.json")
	src := `{"version": 1, "fitness": {"fields": [{"name": "models.Job.State writers", "gate": "off"}]}}`
	if err = os.WriteFile(model, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	c := cli{
		stdout: &stdout,
		stderr: io.Discard,
		logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
	}
	to := filepath.Join(t.TempDir(), "proposals")
	cfg := &config.Config{Out: out, To: to, Format: config.ExportAnarch, AnarchModel: model}
	if code := c.exportAnarch(t.Context(), cfg, nil); code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "1 proposed rule(s) written") {
		t.Errorf("stdout = %q", stdout.String())
	}
	written, err := os.ReadFile(filepath.Clean(filepath.Join(to, anarch.FileName)))
	if err != nil {
		t.Fatal(err)
	}
	var f anarch.File
	if err = json.Unmarshal(written, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Proposals) != 1 ||
		f.Proposals[0].Fitness.Fields[0].Name != "models.Run.Status writers" {
		t.Errorf("proposals = %s", written)
	}
	if code := c.exportAnarch(t.Context(), cfg, nil); code != exitFailure {
		t.Errorf("a second export into %s: exit = %d, want %d", to, code, exitFailure)
	}
}
