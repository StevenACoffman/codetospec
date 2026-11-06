package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ui"
)

// The flags and command the tests pass more than once.
const (
	outFlag   = "--out"
	toFlag    = "--to"
	exportCmd = "export"
	cerbosFmt = "cerbos"
)

// loginFacts names one access check guarding Query.invoice: logged in, or
// rejected.
const loginFacts = `{"schema":"codetospec/facts/v1","facts":[
{"kind":"rule","id":"rule.webapp.authz.invoice","origin":"webapp","certainty":"static",
 "source":{"path":"app/Models/Invoice.php","lines":"1-3"},
 "attrs":{"slug":"authz-invoice","title":"Access control: Query.invoice",
  "requirement":"IF the caller is not logged in, THEN the system shall reject Query.invoice.",
  "ears":"unwanted","nature":"business","domain":"billing","operation":"Query.invoice",
  "checks":"acl.IsLoggedIn","check_args":"[{\"check\":\"acl.IsLoggedIn\",\"var\":\"caller.loggedIn\",\"means\":\"login\"}]",
  "condition":"{\"not\":{\"var\":\"caller.loggedIn\",\"op\":\"equal\",\"value\":true}}"}},
{"kind":"access_check","id":"access_check.webapp.acl.IsLoggedIn","origin":"webapp","certainty":"static",
 "source":{"path":"","lines":""},"attrs":{"name":"acl.IsLoggedIn","form":"acl"}}]}`

// guardedGraph builds, through the real pipeline, a graph whose extractor
// facts are facts (a facts file), and returns its directory.
func guardedGraph(t *testing.T, facts string) string {
	t.Helper()
	server := mockLLM(t)
	t.Cleanup(server.Close)
	outDir := t.TempDir()
	cfg := e2eConfig(t, server.URL, outDir)
	cfg.Lang = "en"
	factsPath := filepath.Join(t.TempDir(), "checks.facts.json")
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.FactsFiles = append(cfg.FactsFiles, factsPath)
	if err := runPipeline(context.Background(), cfg, ui.Discard{},
		slog.New(slog.NewTextHandler(t.Output(), nil))); err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	return outDir
}

// command runs codetospec with args and returns its stdout.
func command(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), args, &stdout, &stderr); code != exitOK {
		t.Fatalf("%v exited %d: %s", args, code, stderr.String())
	}
	return stdout.String()
}

// TestPoliciesCommand: `policies` lists the guard and the check,
// deterministic (two runs give the same bytes), and its JSON parses.
func TestPoliciesCommand(t *testing.T) {
	outDir := guardedGraph(t, loginFacts)
	first := command(t, "policies", outFlag, outDir)
	if again := command(t, "policies", outFlag, outDir); again != first {
		t.Error("two runs of policies differ")
	}
	for _, want := range []string{"rule.billing.authz-invoice", "acl.IsLoggedIn [check]"} {
		if !strings.Contains(first, want) {
			t.Errorf("inventory lacks %q:\n%s", want, first)
		}
	}
	var inv map[string]any
	if err := json.Unmarshal(
		[]byte(command(t, "policies", outFlag, outDir, "--format", "json")),
		&inv,
	); err != nil {
		t.Errorf("--format json: %v", err)
	}
}

// TestExportPolicyFormats: the guard becomes a deny in Cerbos and a forbid
// in the Cedar policy language, each traced to its rule id.
func TestExportPolicyFormats(t *testing.T) {
	outDir := guardedGraph(t, loginFacts)
	cases := []struct {
		format, file string
		want         []string
	}{
		{
			cerbosFmt, filepath.Join("resource_policies", filepath.Base(outDir)+".yaml"),
			[]string{"name: rule.billing.authz-invoice", "EFFECT_DENY", "P.attr.loggedIn == true"},
		},
		{
			"cedar-policy", filepath.Base(outDir) + ".cedar",
			[]string{`@id("rule.billing.authz-invoice")`, "forbid", "principal.loggedIn"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			to := filepath.Join(t.TempDir(), tc.format)
			command(t, exportCmd, outFlag, outDir, toFlag, to, "--format", tc.format)
			written := filepath.Join(to, tc.file)
			data, err := os.ReadFile(written) //nolint:gosec // the test's own output
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(data), want) {
					t.Errorf("%s lacks %q:\n%s", tc.file, want, data)
				}
			}
		})
	}
}

// TestExportJoinsCapabilities: a service names a capability another graph
// (admin's) registers; alone its guard can't translate, joined with
// --capabilities-from it becomes a deny on the stored name.
func TestExportJoinsCapabilities(t *testing.T) {
	outDir := guardedGraph(t, `{"schema":"codetospec/facts/v1","facts":[
{"kind":"rule","id":"rule.webapp.authz.notes","origin":"webapp","certainty":"static",
 "source":{"path":"app/Models/Invoice.php","lines":"1-3"},
 "attrs":{"slug":"authz-notes","title":"Access control: Query.notes",
  "requirement":"IF the caller lacks permission CanAddUserNote, THEN the system shall reject Query.notes.",
  "ears":"unwanted","nature":"business","domain":"billing","operation":"Query.notes",
  "checks":"acl.ActorHasPermission",
  "check_args":"[{\"check\":\"acl.ActorHasPermission\",\"var\":\"caller.hasPermission.CanAddUserNote\",\"means\":\"capability\",\"scope\":\"any\",\"capability\":\"CanAddUserNote\",\"matcher\":\"acl.AnyScope\"}]",
  "condition":"{\"not\":{\"var\":\"caller.hasPermission.CanAddUserNote\",\"op\":\"equal\",\"value\":true}}"}}]}`)
	admin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(admin, ".codetospec"), 0o750); err != nil {
		t.Fatal(err)
	}
	adminFacts := `{"schema":"codetospec/facts/v1","facts":[{"kind":"capability","id":"capability.1",
 "origin":"webapp","certainty":"static","source":{"path":"caps.go","lines":"1-1"},
 "attrs":{"name":"CanAddUserNote","value":"can_add_user_note"}}]}`
	if err := os.WriteFile(
		filepath.Join(admin, ".codetospec", "facts.json"),
		[]byte(adminFacts),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	alone := command(
		t,
		exportCmd,
		outFlag,
		outDir,
		toFlag,
		filepath.Join(t.TempDir(), "a"),
		"--format",
		cerbosFmt,
	)
	joined := command(
		t,
		exportCmd,
		outFlag,
		outDir,
		toFlag,
		filepath.Join(t.TempDir(), "b"),
		"--format",
		cerbosFmt,
		"--capabilities-from",
		admin,
	)
	if !strings.Contains(alone, "0 deny rule(s)") || !strings.Contains(joined, "1 deny rule(s)") {
		t.Errorf("alone: %q, joined: %q; want 0 then 1 deny rule", alone, joined)
	}
}

// TestExportCheckers: a check the extractor gave no meaning stays out
// until a checkers file pins it; a bad checkers file fails the export
// before anything is written.
func TestExportCheckers(t *testing.T) {
	outDir := guardedGraph(t, `{"schema":"codetospec/facts/v1","facts":[
{"kind":"rule","id":"rule.webapp.authz.cdi","origin":"webapp","certainty":"static",
 "source":{"path":"app/Models/Invoice.php","lines":"1-3"},
 "attrs":{"slug":"authz-cdi","title":"Access control: Query.cdi",
  "requirement":"IF permissions.CanViewCDI fails, THEN the system shall reject Query.cdi.",
  "ears":"unwanted","nature":"business","domain":"billing","operation":"Query.cdi",
  "checks":"permissions.CanViewCDI",
  "check_args":"[{\"check\":\"permissions.CanViewCDI\",\"var\":\"permissions.CanViewCDI.fails\",\"fails\":true}]",
  "condition":"{\"var\":\"permissions.CanViewCDI.fails\",\"op\":\"equal\",\"value\":true}"}}]}`)
	dir := t.TempDir()
	good := filepath.Join(dir, "checkers.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	for path, data := range map[string]string{
		good: "permissions.CanViewCDI: {means: relation, relation: teacher_of}\n",
		bad:  "permissions.CanViewCDI: {means: magic}\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	export := func(extra ...string) string {
		to := filepath.Join(t.TempDir(), "p")
		return command(t, append([]string{
			exportCmd, outFlag, outDir, toFlag, to,
			"--format", cerbosFmt,
		}, extra...)...)
	}
	if alone, pinned := export(), export(
		"--checkers",
		good,
	); !strings.Contains(
		alone,
		"0 deny rule(s)",
	) ||
		!strings.Contains(pinned, "1 deny rule(s)") {
		t.Errorf("alone: %q, pinned: %q; want 0 then 1 deny rule", alone, pinned)
	}
	var stdout, stderr bytes.Buffer
	to := filepath.Join(t.TempDir(), "p")
	if code := run(context.Background(), []string{
		exportCmd, outFlag, outDir, toFlag, to,
		"--format", cerbosFmt, "--checkers", bad,
	}, &stdout, &stderr); code == exitOK {
		t.Error("a bad checkers file exported")
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Errorf("a failed export wrote %s", to)
	}
}
