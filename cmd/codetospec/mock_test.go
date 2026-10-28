package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
)

// mockLLM is a deterministic OpenAI-compatible endpoint: it answers map
// prompts with one rule per chunk containing business code, reduce prompts
// by consolidating the candidates verbatim (a controller's rules
// technical), crosscheck prompts with "supported" and quality prompts with
// "security".
func mockLLM(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) < 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		content, err := mockReply(req.Messages[0].Content, req.Messages[1].Content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": content}},
			},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(resp); encodeErr != nil {
			t.Errorf("mock: encode response: %v", encodeErr)
		}
	}))
}

// mockReply answers one prompt, recognized by its system prompt.
func mockReply(system, user string) (string, error) {
	switch {
	case strings.Contains(system, "software archaeologist"):
		return mockMap(user)
	case strings.Contains(system, "requirements engineer"):
		return mockReduce(user)
	case strings.Contains(system, "adversarial code reviewer"):
		if !strings.Contains(user, "CITED SOURCE") {
			return "", errors.New("no cited source in crosscheck prompt")
		}
		return `{"verdict": "supported", "reason": "Les lignes citées implémentent la règle."}`, nil
	case strings.Contains(system, "ISO 25010 quality characteristic"):
		return mockQuality(user)
	default:
		return "", errors.New("unknown prompt")
	}
}

// mockFileHeader is the map prompt's chunk header.
var mockFileHeader = regexp.MustCompile(`FILE: (\S+) \(lines (\d+)-(\d+)\)`)

// mockMap states one rule for a chunk holding a public function, citing
// the whole chunk and every allowed entity and endpoint.
func mockMap(user string) (string, error) {
	m := mockFileHeader.FindStringSubmatch(user)
	if m == nil {
		return "", errors.New("no FILE header")
	}
	payload := map[string]any{"chunk_summary": "Chunk analysé.", "rules": []any{}}
	if strings.Contains(user, "public function") {
		entities, err := promptList(user, "ALLOWED_ENTITIES: ")
		if err != nil {
			return "", err
		}
		endpoints, err := promptList(user, "ALLOWED_ENDPOINTS: ")
		if err != nil {
			return "", err
		}
		payload["rules"] = []any{map[string]any{
			"title":      "Règle de " + m[1],
			"ears":       mockEARS(m[1]),
			"citations":  []any{map[string]any{"path": m[1], "lines": m[2] + "-" + m[3]}},
			"entities":   entities,
			"endpoints":  endpoints,
			"nature":     "business",
			"origin":     "explicit",
			"confidence": 0.9,
		}}
	}
	data, err := json.Marshal(payload)
	return string(data), err
}

// mockReduce keeps every candidate as a rule, with two acceptance criteria.
func mockReduce(user string) (string, error) {
	_, candidateJSON, ok := strings.Cut(user, "CANDIDATE_RULES:\n")
	if !ok {
		return "", errors.New("no candidates")
	}
	var candidates []mapper.Rule
	if err := json.Unmarshal([]byte(candidateJSON), &candidates); err != nil {
		return "", err
	}
	rules := make([]any, 0, len(candidates))
	for i, c := range candidates {
		rules = append(rules, map[string]any{
			"slug":      "regle-" + string(rune('a'+i)),
			"title":     c.Title,
			"ears":      c.EARS,
			"rationale": "Consolidée depuis le code.",
			"citations": c.Citations,
			"entities":  c.Entities,
			"endpoints": c.Endpoints,
			"acceptance_criteria": []string{
				"Le calcul nominal est correct.",
				"Le cas limite est couvert.",
			},
			"nature":     mockNature(c.Citations),
			"origin":     "explicit",
			"confidence": 0.9,
		})
	}
	data, err := json.Marshal(
		map[string]any{"domain_summary": "Domaine consolidé.", "rules": rules},
	)
	return string(data), err
}

// mockNature makes a controller's rules technical, so the quality pass
// has rules to classify.
func mockNature(citations []extract.Ref) string {
	for _, c := range citations {
		if strings.Contains(c.Path, "/Http/") {
			return "technical"
		}
	}
	return "business"
}

// mockQuality classifies every listed rule as security.
func mockQuality(user string) (string, error) {
	var rules []any
	for line := range strings.SplitSeq(user, "\n") {
		if label, _, found := strings.Cut(line, ". "); found && strings.HasPrefix(label, "R") {
			rules = append(rules, map[string]any{"label": label, "quality_attribute": "security"})
		}
	}
	if len(rules) == 0 {
		return "", errors.New("no rule in quality prompt")
	}
	data, err := json.Marshal(struct {
		Rules []any `json:"rules"`
	}{rules})
	return string(data), err
}

// promptList reads the JSON list a prompt line starting with prefix holds.
func promptList(prompt, prefix string) ([]string, error) {
	for line := range strings.SplitSeq(prompt, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			var list []string
			if err := json.Unmarshal([]byte(rest), &list); err != nil {
				return nil, fmt.Errorf("mock: %s list %q: %w", prefix, rest, err)
			}
			return list, nil
		}
	}
	return nil, nil
}

// mockEARS are the EARS components the mock map step states for a file.
// The ProrataCalculator rule carries a planted wrong threshold (15 is not in
// the code) that the mechanical grounding check must flag.
func mockEARS(path string) map[string]any {
	prorata := []map[string]string{{"text": "facturer au prorata", "action": "facturer"}}
	if strings.HasSuffix(path, "ProrataCalculator.php") {
		return map[string]any{
			"if": "le jour d'activation dépasse 15", "shall": prorata,
		}
	}
	return map[string]any{
		"when": "un abonné active en cours de mois", "shall": prorata,
	}
}
