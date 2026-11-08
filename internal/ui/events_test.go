package ui_test

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ui"
)

// TestPlainLogsJudgePasses: the quality and acceptance passes log a
// progress line per unit, like the crosscheck, so a quiet log is a stall.
func TestPlainLogsJudgePasses(t *testing.T) {
	var logged bytes.Buffer
	p := ui.Plain{Log: slog.New(slog.NewTextHandler(&logged, nil)), Out: io.Discard}
	p.Emit(ui.QualityBatch{Done: 1, Total: 3})
	p.Emit(ui.AcceptanceUnit{RuleID: "rule.billing.refund", Done: 2, Total: 9})
	for _, want := range []string{
		`msg="quality progress" done=1 total=3`,
		`msg="acceptance progress" rule=rule.billing.refund done=2 total=9`,
	} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logged.String())
		}
	}
}
