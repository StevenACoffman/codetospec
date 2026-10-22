package crosscheck_test

import (
	"encoding/json"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
)

// TestEvalPlantedFaults measures how the LLM crosscheck judges rules over
// the real fixture code: true rules (controls) it should support, and rules
// with a known planted fault it should refute. It is opt-in
// (CODETOSPEC_EVAL=1 go test ./internal/crosscheck -run Eval -v, with
// LLM_BASE_URL, LLM_MODEL and optionally LLM_API_KEY set) because it calls
// a live model, and it reports rather than asserts, since verdicts vary
// between models and runs.
//
// CODETOSPEC_EVAL_REPEAT=n runs the cases n times (default 1), to measure
// how often a verdict flips. CODETOSPEC_EVAL_RECORD=path writes the run as
// JSON; CODETOSPEC_EVAL_BASELINE=path compares it with such a record.
//
// Adopt a prompt or model change only when, over at least 3 repeats, its
// balanced accuracy is no lower than the baseline's, its false-alarm rate
// no higher, and no more cases flip. Balanced accuracy is the mean of the
// catch rate and the control pass rate: a reviewer that refutes everything
// catches every fault yet scores 0.5, as does one that supports everything.
func TestEvalPlantedFaults(t *testing.T) {
	if os.Getenv("CODETOSPEC_EVAL") != "1" {
		t.Skip("set CODETOSPEC_EVAL=1 to run against a live LLM")
	}
	baseURL, model := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL")
	if baseURL == "" || model == "" {
		t.Fatal("CODETOSPEC_EVAL needs LLM_BASE_URL and LLM_MODEL")
	}
	repeat := 1
	if s := os.Getenv("CODETOSPEC_EVAL_REPEAT"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			t.Fatalf("CODETOSPEC_EVAL_REPEAT=%q is not a positive count", s)
		}
		repeat = n
	}

	cases := evalCases()
	nodes := make([]graph.Node, 0, len(cases))
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		nodes = append(nodes, graph.Node{
			ID:      "rule.eval." + name,
			Type:    graph.TypeRule,
			Title:   name,
			Body:    "**Exigence (EARS)** : " + cases[name].requirement,
			Sources: []extract.Ref{{Path: prorataFile, Lines: "11-24"}},
		})
	}
	runs := make([]map[string]crosscheck.Verdict, 0, repeat)
	for range repeat {
		checker := &crosscheck.Checker{
			Log:     slog.New(slog.NewTextHandler(t.Output(), nil)),
			LLM:     llm.NewClient(baseURL, os.Getenv("LLM_API_KEY"), model, 4096),
			Model:   model,
			Lang:    "fr",
			Workers: 2,
			OutDir:  t.TempDir(), // never reuse cached verdicts in a measurement
			SrcRoot: "../../testdata/fixture",
		}
		verdicts, err := checker.Run(t.Context(), nodes)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		runs = append(runs, verdicts)
	}

	score := scoreEval(cases, runs)
	logEval(t, model, cases, runs, &score)
	if path := os.Getenv("CODETOSPEC_EVAL_RECORD"); path != "" {
		writeEvalRecord(t, path, &evalRecord{Model: model, Score: score, Runs: runs})
	}
	if path := os.Getenv("CODETOSPEC_EVAL_BASELINE"); path != "" {
		compareEvalBaseline(t, path, &score)
	}
}

// evalCases are the controls and planted faults, over ProrataCalculator's
// calculate (lines 11-24 of the fixture).
func evalCases() map[string]plantedRule {
	return map[string]plantedRule{
		"control-full-month": {
			"SI le jour d'activation est inférieur ou égal à 1, ALORS le systeme doit " +
				"facturer le montant mensuel complet.", false,
		},
		"control-negative": {
			"SI le montant mensuel est négatif, ALORS le systeme doit rejeter le calcul " +
				`avec "monthly amount must not be negative".`, false,
		},
		"control-rounding": {
			"Le systeme doit arrondir le montant au prorata à 2 décimales.", false,
		},
		"control-prorata": {
			"QUAND un abonnement est activé après le premier jour du mois, le systeme doit " +
				"facturer le montant mensuel au prorata des jours restants, jour d'activation " +
				"inclus.", false,
		},
		"changed-threshold": {
			"SI le jour d'activation est inférieur ou égal à 5, ALORS le systeme doit " +
				"facturer le montant mensuel complet.", true,
		},
		"negated-condition": {
			"SI le montant mensuel est supérieur ou égal à 0, ALORS le systeme doit " +
				"rejeter le calcul.", true,
		},
		"invented-message": {
			`SI le montant mensuel est négatif, ALORS le systeme doit rejeter le calcul ` +
				`avec "amount too large".`, true,
		},
		"wrong-actor": {
			"QUAND un administrateur active un abonnement, le systeme doit " +
				"facturer le montant mensuel complet.", true,
		},
	}
}

// plantedRule is one eval case: a requirement over the fixture code and
// whether a fault was planted in it.
type plantedRule struct {
	requirement string
	faulty      bool
}

// evalScore is what a measurement found, over every run.
type evalScore struct {
	Faults      int `json:"faults"`       // faulty case judgments
	Caught      int `json:"caught"`       // faulty ones refuted
	Controls    int `json:"controls"`     // control case judgments
	FalseAlarms int `json:"false_alarms"` // controls not supported
	// Failed counts calls that gave no verdict; each is counted as a wrong
	// answer (a miss, or a false alarm), never left out.
	Failed           int      `json:"failed"`
	CatchRate        float64  `json:"catch_rate"`
	FalseAlarmRate   float64  `json:"false_alarm_rate"`
	BalancedAccuracy float64  `json:"balanced_accuracy"`
	Flipped          []string `json:"flipped"` // cases judged differently across runs
}

// scoreEval scores the verdicts of each run: a faulty case is right when
// refuted (unsupported or partial), a control when supported; a failed call
// is wrong either way. A case flips when its runs do not all agree.
//
// Requires: every run judged the cases of cases, ids rule.eval.<name>.
// Ensures:  rates are 0 when there is nothing to rate; Flipped is sorted.
func scoreEval(cases map[string]plantedRule, runs []map[string]crosscheck.Verdict) evalScore {
	var s evalScore
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		outcomes := map[string]bool{}
		for _, run := range runs {
			v := run["rule.eval."+name]
			outcomes[s.judge(cases[name].faulty, &v)] = true
		}
		if len(outcomes) > 1 {
			s.Flipped = append(s.Flipped, name)
		}
	}
	s.CatchRate = ratio(s.Caught, s.Faults)
	s.FalseAlarmRate = ratio(s.FalseAlarms, s.Controls)
	s.BalancedAccuracy = (s.CatchRate + 1 - s.FalseAlarmRate) / 2
	return s
}

// judge counts one judgment of a case and returns its outcome: the
// verdict, or "failed".
func (s *evalScore) judge(faulty bool, v *crosscheck.Verdict) string {
	if v.Failed {
		s.Failed++
	}
	right := !v.Failed && v.Verdict == crosscheck.VerdictSupported
	if faulty {
		right = !v.Failed &&
			(v.Verdict == crosscheck.VerdictUnsupported || v.Verdict == crosscheck.VerdictPartial)
		s.Faults++
		if right {
			s.Caught++
		}
	} else {
		s.Controls++
		if !right {
			s.FalseAlarms++
		}
	}
	if v.Failed {
		return "failed"
	}
	return v.Verdict
}

func ratio(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// logEval logs each case's verdicts and the score.
func logEval(
	t *testing.T,
	model string,
	cases map[string]plantedRule,
	runs []map[string]crosscheck.Verdict,
	s *evalScore,
) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		for i, run := range runs {
			v := run["rule.eval."+name]
			if v.Failed {
				t.Logf("%-20s run %d failed: %s", name, i+1, v.Error)
				continue
			}
			t.Logf("%-20s run %d faulty=%-5v verdict=%s: %s",
				name, i+1, cases[name].faulty, v.Verdict, v.Reason)
		}
	}
	t.Logf("model %s, %d run(s): caught %d/%d faults, %d/%d false alarms, %d failed calls",
		model, len(runs), s.Caught, s.Faults, s.FalseAlarms, s.Controls, s.Failed)
	t.Logf("balanced accuracy %.2f (always refute: 0.50), flipped: %v",
		s.BalancedAccuracy, s.Flipped)
}

// evalRecord is a measurement as CODETOSPEC_EVAL_RECORD writes it.
type evalRecord struct {
	Model string                          `json:"model"`
	Score evalScore                       `json:"score"`
	Runs  []map[string]crosscheck.Verdict `json:"runs"`
}

func writeEvalRecord(t *testing.T, path string, r *evalRecord) {
	t.Helper()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatalf("encode eval record: %v", err)
	}
	// The path is the developer's own CODETOSPEC_EVAL_RECORD.
	if err = os.WriteFile( //nolint:gosec // G703: see above.
		filepath.Clean(path), append(data, '\n'), 0o600,
	); err != nil {
		t.Fatalf("write eval record: %v", err)
	}
}

// compareEvalBaseline logs whether s meets the adoption bar against the
// record at path.
func compareEvalBaseline(t *testing.T, path string, s *evalScore) {
	t.Helper()
	// The path is the developer's own CODETOSPEC_EVAL_BASELINE.
	data, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G703: see above.
	if err != nil {
		t.Fatalf("read eval baseline: %v", err)
	}
	var base evalRecord
	if err = json.Unmarshal(data, &base); err != nil {
		t.Fatalf("parse eval baseline %s: %v", path, err)
	}
	meets := s.BalancedAccuracy >= base.Score.BalancedAccuracy &&
		s.FalseAlarmRate <= base.Score.FalseAlarmRate &&
		len(s.Flipped) <= len(base.Score.Flipped)
	t.Logf("vs baseline %s (%s): balanced accuracy %+.2f, false-alarm rate %+.2f, "+
		"flipped %d → %d; meets the adoption bar: %v",
		path, base.Model,
		s.BalancedAccuracy-base.Score.BalancedAccuracy,
		s.FalseAlarmRate-base.Score.FalseAlarmRate,
		len(base.Score.Flipped), len(s.Flipped), meets)
}

// TestScoreEval pins the scoring the eval reports: a reviewer that refutes
// everything scores no better than chance, a failed call is a wrong answer,
// and disagreeing runs flip a case.
func TestScoreEval(t *testing.T) {
	cases := map[string]plantedRule{
		"control": {"r", false},
		"fault":   {"r", true},
	}
	verdict := func(v string) crosscheck.Verdict { return crosscheck.Verdict{Verdict: v} }
	failed := crosscheck.Verdict{Failed: true}
	run := func(control, fault crosscheck.Verdict) map[string]crosscheck.Verdict {
		return map[string]crosscheck.Verdict{"rule.eval.control": control, "rule.eval.fault": fault}
	}
	supported, unsupported := verdict(
		crosscheck.VerdictSupported,
	), verdict(
		crosscheck.VerdictUnsupported,
	)

	tests := map[string]struct {
		runs         []map[string]crosscheck.Verdict
		wantBalanced float64
		wantFlipped  []string
	}{
		"perfect": {[]map[string]crosscheck.Verdict{run(supported, unsupported)}, 1, nil},
		"always refute": {
			[]map[string]crosscheck.Verdict{run(unsupported, unsupported)},
			0.5,
			nil,
		},
		"always support":  {[]map[string]crosscheck.Verdict{run(supported, supported)}, 0.5, nil},
		"failed is wrong": {[]map[string]crosscheck.Verdict{run(failed, failed)}, 0, nil},
		"flip": {
			[]map[string]crosscheck.Verdict{run(supported, unsupported), run(supported, supported)},
			0.75,
			[]string{"fault"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := scoreEval(cases, tc.runs)
			if math.Abs(got.BalancedAccuracy-tc.wantBalanced) > 1e-9 ||
				!slices.Equal(got.Flipped, tc.wantFlipped) {
				t.Errorf("balanced accuracy %.2f, flipped %v; want %.2f, %v",
					got.BalancedAccuracy, got.Flipped, tc.wantBalanced, tc.wantFlipped)
			}
		})
	}
}
