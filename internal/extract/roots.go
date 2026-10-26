package extract

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
)

// ErrUnknownRoot is returned for a Ref naming a root that was not given.
var ErrUnknownRoot = errors.New("unknown citation root")

// Roots resolves a Ref to its file: Src (--src) for Root "", and the extra
// source trees named by --ref-root otherwise (webapp's shared pkg/ for the
// rules a service inherits). It is the only place that knows where a
// cited file lives: grounding, drift and verify read through it.
type Roots struct {
	Src   fs.FS
	Extra map[string]fs.FS // by root name
}

// ReadFile returns the content of the file ref cites.
func (r Roots) ReadFile(ref Ref) ([]byte, error) {
	fsys, err := r.fs(ref.Root)
	if err != nil {
		return nil, err
	}
	name := path.Clean(ref.Path)
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("path %q escapes its root", ref.Path)
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ref.File(), err)
	}
	return data, nil
}

func (r Roots) fs(root string) (fs.FS, error) {
	if root == "" {
		return r.Src, nil
	}
	if fsys, ok := r.Extra[root]; ok {
		return fsys, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownRoot, root)
}

// File names the file a Ref cites, for messages and keys: its path, or
// "@root/path" outside --src.
func (r Ref) File() string {
	if r.Root == "" {
		return r.Path
	}
	return "@" + r.Root + "/" + r.Path
}

// CheckRoots reports the citation roots facts use that roots does not name.
// A rule citing an unknown root would fail verification only after the
// LLM phases; checked first, the run stops before spending on them.
//
// Ensures: nil when every rooted fact can be read; otherwise an error naming
// each missing root once, sorted, with a fact citing it.
func (r Roots) CheckRoots(facts []Fact) error {
	missing := map[string]string{} // root → a fact citing it
	for i := range facts {
		root := facts[i].Source.Root
		if _, known := r.Extra[root]; root != "" && !known && missing[root] == "" {
			missing[root] = facts[i].ID
		}
	}
	if len(missing) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(missing))
	for _, root := range slices.Sorted(maps.Keys(missing)) {
		reasons = append(reasons, fmt.Sprintf("%s (cited by %s): add --ref-root %s=<dir>",
			root, missing[root], root))
	}
	return fmt.Errorf("%w: %s", ErrUnknownRoot, strings.Join(reasons, "; "))
}
