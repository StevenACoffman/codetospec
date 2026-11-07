package quality_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/quality"
)

// promiseURL is Dalpiaz et al.'s relabeled PROMISE NFR set (RE'19,
// explainable-re/RE-2019-Materials, GPL-3.0): each requirement with its
// PROMISE class and, labeled apart, whether it has a functional and a
// quality aspect. Downloaded at evaluation time, never committed.
const promiseURL = "https://raw.githubusercontent.com/explainable-re/RE-2019-Materials/" +
	"master/Manually%20tagged%20datasets/promise-reclass.csv"

// promiseAttributes maps each PROMISE class to the attributes --quality may
// answer for it, "" for none: F is plain functionality. O (operational) is
// decided first, since ISO 25010 has no such characteristic: PROMISE's O
// requirements state the operating environment (platforms, other systems
// to run with), so portability or compatibility is right.
func promiseAttributes() map[string][]string {
	return map[string][]string{
		"F":  {""},
		"A":  {quality.Availability},
		"FT": {quality.Reliability},
		"L":  {quality.Compliance},
		"LF": {quality.Usability},
		"US": {quality.Usability},
		"MN": {quality.Maintainability},
		"PE": {quality.PerformanceEfficiency},
		"PO": {quality.Portability},
		"SC": {quality.Scalability},
		"SE": {quality.Security},
		"O":  {quality.Portability, quality.Compatibility},
	}
}

// labeled is one requirement of the set.
type labeled struct {
	Text      string
	Class     string // PROMISE's
	IsQuality bool   // Dalpiaz's separate label
}

// qualityScore is one evaluation, as CODETOSPEC_EVAL_RECORD writes it.
type qualityScore struct {
	Total int `json:"total"`
	// Correct counts answers among their class's attributes, PerClass
	// each class's [correct, total].
	Correct  int               `json:"correct"`
	PerClass map[string][2]int `json:"per_class"`
	// Detection compares "an attribute" with Dalpiaz's IsQuality: true
	// and false positives and negatives.
	TruePositive  int `json:"true_positive"`
	FalsePositive int `json:"false_positive"`
	FalseNegative int `json:"false_negative"`
	TrueNegative  int `json:"true_negative"`
	Unanswered    int `json:"unanswered"` // a failed batch left it out
}

// scoreQuality scores answers (by index of reqs; absent when its batch
// failed) against the labels.
func scoreQuality(reqs []labeled, answers map[int]string) qualityScore {
	s := qualityScore{PerClass: map[string][2]int{}}
	accepted := promiseAttributes()
	for i, r := range reqs {
		answer, ok := answers[i]
		if !ok {
			s.Unanswered++
			continue
		}
		s.Total++
		pc := s.PerClass[r.Class]
		pc[1]++
		if slices.Contains(accepted[r.Class], answer) {
			s.Correct++
			pc[0]++
		}
		s.PerClass[r.Class] = pc
		switch {
		case answer != "" && r.IsQuality:
			s.TruePositive++
		case answer != "":
			s.FalsePositive++
		case r.IsQuality:
			s.FalseNegative++
		default:
			s.TrueNegative++
		}
	}
	return s
}

// TestScoreQuality pins scoreQuality: an answer among its class's
// attributes is correct (O takes either of two), and detection compares
// "an attribute" with IsQuality, a failed answer counted apart.
func TestScoreQuality(t *testing.T) {
	reqs := []labeled{
		{Class: "PE", IsQuality: true},
		{Class: "O", IsQuality: true},
		{Class: "F", IsQuality: false},
		{Class: "F", IsQuality: true}, // functional with a quality aspect
		{Class: "SE", IsQuality: true},
	}
	got := scoreQuality(reqs, map[int]string{
		0: quality.PerformanceEfficiency, 1: quality.Compatibility, 2: quality.Security, 3: "",
	})
	want := qualityScore{
		Total: 4, Correct: 3,
		PerClass:     map[string][2]int{"PE": {1, 1}, "O": {1, 1}, "F": {1, 2}},
		TruePositive: 2, FalsePositive: 1, FalseNegative: 1, Unanswered: 1,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("score = %+v, want %+v", got, want)
	}
}

// TestEvalPromise measures --quality on the PROMISE set (opt-in, calls the
// model and the network: CODETOSPEC_EVAL=1 go test ./internal/quality -run
// EvalPromise -v, with LLM_BASE_URL, LLM_MODEL and optionally
// LLM_API_KEY). CODETOSPEC_EVAL_RECORD=path writes the score as JSON.
func TestEvalPromise(t *testing.T) {
	if os.Getenv("CODETOSPEC_EVAL") != "1" {
		t.Skip("set CODETOSPEC_EVAL=1 to run against a live LLM")
	}
	baseURL, model := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL")
	if baseURL == "" || model == "" {
		t.Fatal("CODETOSPEC_EVAL needs LLM_BASE_URL and LLM_MODEL")
	}
	reqs := downloadPromise(t)
	text := locale.For("en")
	nodes := make([]graph.Node, len(reqs))
	for i, r := range reqs {
		nodes[i] = graph.Node{
			ID: fmt.Sprintf("rule.promise.r%04d", i), Type: graph.TypeRule,
			Body: text.Requirement + r.Text,
		}
	}
	c := quality.Classifier{
		LLM:       llm.NewClient(baseURL, os.Getenv("LLM_API_KEY"), model, 4096),
		Log:       slog.New(slog.NewTextHandler(t.Output(), nil)),
		Model:     model,
		Workers:   4,
		CachePath: t.TempDir() + "/quality.json",
	}
	byID, err := c.Run(context.Background(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	answers := map[int]string{}
	for i := range nodes {
		if a, ok := byID[nodes[i].ID]; ok {
			answers[i] = a
		}
	}
	s := scoreQuality(reqs, answers)
	t.Logf(
		"accuracy %d/%d; detection TP %d FP %d FN %d TN %d; unanswered %d",
		s.Correct,
		s.Total,
		s.TruePositive,
		s.FalsePositive,
		s.FalseNegative,
		s.TrueNegative,
		s.Unanswered,
	)
	for _, class := range slices.Sorted(maps.Keys(s.PerClass)) {
		t.Logf("  %-2s %d/%d", class, s.PerClass[class][0], s.PerClass[class][1])
	}
	if path := os.Getenv("CODETOSPEC_EVAL_RECORD"); path != "" {
		writeRecord(t, path, &s)
	}
}

// writeRecord writes s as JSON to path, the developer's own
// CODETOSPEC_EVAL_RECORD.
func writeRecord(t *testing.T, path string, s *qualityScore) {
	t.Helper()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// The path is the developer's own CODETOSPEC_EVAL_RECORD.
	if err = os.WriteFile( //nolint:gosec // G703: see above.
		filepath.Clean(path), append(data, '\n'), 0o600,
	); err != nil {
		t.Fatal(err)
	}
}

// downloadPromise fetches and parses the set.
func downloadPromise(t *testing.T) []labeled {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, promiseURL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", promiseURL, resp.Status)
	}
	reqs, err := parsePromise(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reqs
}

// parsePromise reads the set's CSV: ProjectID, RequirementText (quoted
// in single quotes), Class, IsFunctional, IsQuality.
func parsePromise(r io.Reader) ([]labeled, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows[0]) < 5 || rows[0][1] != "RequirementText" {
		return nil, fmt.Errorf("unexpected header %v", rows[:min(1, len(rows))])
	}
	out := make([]labeled, 0, len(rows)-1)
	for _, row := range rows[1:] {
		isQuality, convErr := strconv.Atoi(row[4])
		if convErr != nil {
			return nil, fmt.Errorf("IsQuality %q: %w", row[4], convErr)
		}
		out = append(out, labeled{
			Text:      strings.Trim(row[1], "'"),
			Class:     row[2],
			IsQuality: isQuality == 1,
		})
	}
	return out, nil
}

// TestParsePromise: the set's rows read, single quotes trimmed.
func TestParsePromise(t *testing.T) {
	got, err := parsePromise(
		strings.NewReader("ProjectID,RequirementText,Class,IsFunctional,IsQuality\n" +
			"1,'The system shall refresh the display every 60 seconds.',PE,1,1\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []labeled{
		{
			Text:      "The system shall refresh the display every 60 seconds.",
			Class:     "PE",
			IsQuality: true,
		},
	}
	if !slices.Equal(got, want) {
		t.Errorf("parsed %+v, want %+v", got, want)
	}
}
