// Package state persists run progress and token costs as JSON, enabling
// resume after interruption.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
)

// PhaseTokens accumulates token usage for one phase: over every run on
// this output, and over the latest run alone (a warm-cache run costs 0).
type PhaseTokens struct {
	Prompt         int `json:"prompt"`
	Completion     int `json:"completion"`
	LastPrompt     int `json:"last_prompt"`
	LastCompletion int `json:"last_completion"`
}

// Phases lists the LLM phases whose tokens State counts, in run order.
func Phases() []string {
	return []string{"map", "reduce", "crosscheck"}
}

// AddTokens counts one LLM call of phase, in the cumulative and the
// latest-run counters.
func (s *State) AddTokens(phase string, prompt, completion int) {
	t := s.Tokens[phase]
	if t == nil {
		t = &PhaseTokens{}
		s.Tokens[phase] = t
	}
	t.Prompt += prompt
	t.Completion += completion
	t.LastPrompt += prompt
	t.LastCompletion += completion
}

// State is the persisted run state.
type State struct {
	Version       int                     `json:"version"`
	SrcPath       string                  `json:"src_path"`
	StartedAt     string                  `json:"started_at"`
	Phase         string                  `json:"phase"`
	ChunksDone    int                     `json:"chunks_done"`
	ChunksFailed  int                     `json:"chunks_failed"`
	ChunksTotal   int                     `json:"chunks_total"`
	DomainsDone   int                     `json:"domains_done"`
	DomainsFailed int                     `json:"domains_failed"`
	DomainsTotal  int                     `json:"domains_total"`
	Extractors    map[string]string       `json:"extractors"`
	Tokens        map[string]*PhaseTokens `json:"tokens"`
}

// sameSource reports whether a state recording recorded may be reused for
// the absolute source src: nothing recorded, a relative path (older
// versions stored --src as given) or the same path.
func sameSource(recorded, src string) bool {
	return recorded == "" || !filepath.IsAbs(recorded) || filepath.Clean(recorded) == src
}

// Store persists State atomically after every mutation.
type Store struct {
	mu   sync.Mutex
	path string
	s    State
}

// ErrOtherSource reports an output directory whose state records another
// source: running there would replace that source's spec.
var ErrOtherSource = errors.New("output holds the spec of another source")

// Open loads the state file when present, otherwise initializes a fresh
// state for srcPath; either way it starts a run, resetting the latest-run
// token counters.
//
// Requires: srcPath names the analyzed repository.
// Ensures:  the state records srcPath made absolute; ErrOtherSource when
// the loaded state records another absolute source (a relative one, from
// an older version, is taken as matching).
func Open(path, srcPath string) (*Store, error) {
	store := &Store{path: path}
	data, err := os.ReadFile(filepath.Clean(path))
	switch {
	case err == nil:
		if unmarshalErr := json.Unmarshal(data, &store.s); unmarshalErr != nil {
			return nil, fmt.Errorf("load state %s: %w", path, unmarshalErr)
		}
	case errors.Is(err, fs.ErrNotExist):
		store.s = State{
			Version:   1,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
	default:
		return nil, fmt.Errorf("load state %s: %w", path, err)
	}
	src, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, fmt.Errorf("resolve source %s: %w", srcPath, err)
	}
	if !sameSource(store.s.SrcPath, src) {
		return nil, fmt.Errorf(
			"%w: %s records %s, not %s; choose another --out, or delete the file to take it over",
			ErrOtherSource, path, store.s.SrcPath, src)
	}
	store.s.SrcPath = src
	if store.s.Extractors == nil {
		store.s.Extractors = map[string]string{}
	}
	if store.s.Tokens == nil {
		store.s.Tokens = map[string]*PhaseTokens{}
	}
	for _, phase := range Phases() {
		if store.s.Tokens[phase] == nil {
			store.s.Tokens[phase] = &PhaseTokens{}
		}
		store.s.Tokens[phase].LastPrompt = 0
		store.s.Tokens[phase].LastCompletion = 0
	}
	return store, nil
}

// Load reads a state file without creating a store; used by the stats command.
func Load(path string) (State, error) {
	var s State
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return s, fmt.Errorf("load state %s: %w", path, err)
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("load state %s: %w", path, err)
	}
	return s, nil
}

// Update mutates the state under lock and saves it atomically.
func (st *Store) Update(fn func(*State)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	return st.saveLocked()
}

// Save persists the current state atomically.
func (st *Store) Save() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.saveLocked()
}

// Snapshot returns a copy of the current state.
func (st *Store) Snapshot() State {
	st.mu.Lock()
	defer st.mu.Unlock()
	snapshot := st.s
	snapshot.Extractors = maps.Clone(st.s.Extractors)
	snapshot.Tokens = make(map[string]*PhaseTokens, len(st.s.Tokens))
	for k, v := range st.s.Tokens {
		copied := *v
		snapshot.Tokens[k] = &copied
	}
	return snapshot
}

func (st *Store) saveLocked() error {
	data, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return atomicfs.WriteFile(st.path, append(data, '\n'))
}
