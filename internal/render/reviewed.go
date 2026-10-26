package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Prior is the output a previous run left in an output directory.
type Prior struct {
	// Nodes are its node files, parsed (as LoadDir reads them).
	Nodes []graph.Node
	// Reviewed are the rule files a person owns, by node id: marked
	// `status: reviewed`, or edited since the run wrote them (their bytes
	// no longer hash as graph.json recorded).
	Reviewed map[string]Reviewed
	// Overwritten are the other node files edited since they were written
	// (domains, entities…): only rule files are kept, so these are
	// rewritten, and the run says so.
	Overwritten []string
}

// Reviewed is a rule file a person owns.
type Reviewed struct {
	Node graph.Node // parsed, status reviewed
	File []byte     // the file as it is written back: as edited, status reviewed
}

// LoadPrior reads the output a previous run left in outDir; an empty Prior
// when there is none. An edited file is told by the hash graph.json
// records; with an older graph.json (no hashes), only files marked
// reviewed are owned.
//
// Ensures: an error only for an unreadable or unparsable node file or
// graph.json.
func LoadPrior(outDir string) (Prior, error) {
	hashes, err := recordedHashes(outDir)
	if err != nil {
		return Prior{}, err
	}
	pattern := filepath.Join(outDir, "nodes", "*", "*.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return Prior{}, fmt.Errorf("glob %s: %w", pattern, err)
	}
	sort.Strings(paths)
	prior := Prior{Reviewed: map[string]Reviewed{}}
	for _, path := range paths {
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return Prior{}, fmt.Errorf("read node %s: %w", path, readErr)
		}
		n, parseErr := ParseNode(string(data))
		if parseErr != nil {
			return Prior{}, fmt.Errorf("parse node %s: %w", path, parseErr)
		}
		prior.Nodes = append(prior.Nodes, n)
		recorded, known := hashes[n.ID]
		edited := known && recorded != fileHash(data)
		switch {
		case n.Status != graph.StatusReviewed && !edited:
		case n.Type != graph.TypeRule:
			prior.Overwritten = append(prior.Overwritten, NodeFile(&n))
		default:
			file := markReviewed(data)
			n.Status = graph.StatusReviewed
			prior.Reviewed[n.ID] = Reviewed{Node: n, File: file}
		}
	}
	return prior, nil
}

// recordedHashes reads the node file hashes graph.json records, by id;
// none when outDir holds no graph.json.
func recordedHashes(outDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Clean(filepath.Join(outDir, "graph.json")))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read graph.json: %w", err)
	}
	var doc struct {
		Nodes map[string]struct {
			Hash string `json:"hash"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse graph.json: %w", err)
	}
	hashes := map[string]string{}
	for id, n := range doc.Nodes {
		if n.Hash != "" {
			hashes[id] = n.Hash
		}
	}
	return hashes, nil
}

// fileHash is the hash graph.json records for a node file's bytes.
func fileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// markReviewed returns data with its frontmatter's status line set to
// reviewed, the rest as it is.
func markReviewed(data []byte) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	for i := 1; i < len(lines) && !bytes.Equal(lines[i], []byte("---\n")); i++ {
		if bytes.HasPrefix(lines[i], []byte("status:")) {
			lines[i] = []byte("status: " + graph.StatusReviewed + "\n")
			break
		}
	}
	return bytes.Join(lines, nil)
}

// KeepReviewed puts each rule a person owns in place of the rule this run
// generated under its id, so their edits survive the run. A kept rule
// keeps its own digest: once its cited code changes, `codetospec drift`
// reports it stale, for its owner to review. A reviewed rule this run no
// longer produces is dropped (the output is meant to be versioned; to
// regenerate a kept rule, delete its file).
//
// Ensures: nodes is not modified; it returns the nodes, the kept rules'
// bytes by id (for Meta.Reviewed) and the dropped rules' ids, sorted.
func KeepReviewed(
	nodes []graph.Node,
	reviewed map[string]Reviewed,
) ([]graph.Node, map[string][]byte, []string) {
	kept := slices.Clone(nodes)
	files := map[string][]byte{}
	var dropped []string
	for i := range kept {
		if r, ok := reviewed[kept[i].ID]; ok && kept[i].Type == graph.TypeRule {
			kept[i] = r.Node
			files[r.Node.ID] = r.File
		}
	}
	for id := range reviewed {
		if _, ok := files[id]; !ok {
			dropped = append(dropped, id)
		}
	}
	slices.Sort(dropped)
	return kept, files, dropped
}
