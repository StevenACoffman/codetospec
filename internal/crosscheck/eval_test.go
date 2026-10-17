package crosscheck_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
)

// TestEvalPlantedFaults measures how often the LLM crosscheck refutes rules
// with a known planted fault, per fault kind, against the real fixture code.
// It is opt-in (CODETOSPEC_EVAL=1 go test ./internal/crosscheck -run Eval -v,
// with LLM_BASE_URL, LLM_MODEL and optionally LLM_API_KEY set) because it
// calls a live model; it reports rather than asserts, since verdicts vary
// between models and runs. Compare the logged rate across prompt or model
// changes before adopting them.
func TestEvalPlantedFaults(t *testing.T) {
	if os.Getenv("CODETOSPEC_EVAL") != "1" {
		t.Skip("set CODETOSPEC_EVAL=1 to run against a live LLM")
	}
	baseURL, model := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL")
	if baseURL == "" || model == "" {
		t.Fatal("CODETOSPEC_EVAL needs LLM_BASE_URL and LLM_MODEL")
	}

	const prorata = "app/Services/Billing/ProrataCalculator.php"
	cases := map[string]plantedRule{
		"control": {
			"SI le jour d'activation est inférieur ou égal à 1, ALORS le systeme doit " +
				"facturer le montant mensuel complet.", false,
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
	nodes := make([]graph.Node, 0, len(cases))
	for name, tc := range cases {
		nodes = append(nodes, graph.Node{
			ID:      "rule.eval." + name,
			Type:    graph.TypeRule,
			Title:   name,
			Body:    "**Exigence (EARS)** : " + tc.requirement,
			Sources: []extract.Ref{{Path: prorata, Lines: "11-24"}},
		})
	}

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

	reportCatchRate(t, model, cases, verdicts)
}

// plantedRule is one eval case: a requirement over the fixture code and
// whether a fault was planted in it.
type plantedRule struct {
	requirement string
	faulty      bool
}

// reportCatchRate logs each verdict and the share of faulty rules refuted.
func reportCatchRate(
	t *testing.T,
	model string,
	cases map[string]plantedRule,
	verdicts map[string]crosscheck.Verdict,
) {
	t.Helper()
	caught, faults := 0, 0
	for name, tc := range cases {
		v := verdicts["rule.eval."+name]
		refuted := v.Verdict == crosscheck.VerdictUnsupported ||
			v.Verdict == crosscheck.VerdictPartial
		switch {
		case v.Failed:
			t.Logf("%-18s unchecked: %s", name, v.Error)
		case tc.faulty:
			faults++
			if refuted {
				caught++
			}
			t.Logf("%-18s caught=%-5v verdict=%s: %s", name, refuted, v.Verdict, v.Reason)
		default:
			t.Logf("%-18s false-alarm=%-5v verdict=%s: %s", name, refuted, v.Verdict, v.Reason)
		}
	}
	t.Logf("model %s caught %d/%d planted faults", model, caught, faults)
}
