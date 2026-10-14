package extract

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
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
