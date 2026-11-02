// Package atomicfs writes files and directories so that neither a reader
// nor a crash part way through ever sees them half written: the new content
// is built beside the target, then renamed over it.
//
// It is the only code that changes the file system: the test beside it
// (onlywriter_test.go) fails on any other call that writes, renames or
// removes, in every module of the repository.
package atomicfs

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/google/renameio/v2"
)

// Modes of what the package creates, the modes the output tree has always
// had: owner-only, as it may quote private code.
const (
	dirPerm  = 0o750
	filePerm = 0o600
)

// WriteFile writes data to path, creating its directory, through a
// temporary file synced and renamed over path (renameio), so neither a
// crash nor a power loss leaves a truncated file behind. The temporary
// file sits beside path, hidden: it shares path's mount, so the rename is
// atomic, and a leftover is skipped by every reader of the tree.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	if err := renameio.WriteFile(path, data, filePerm, renameio.WithTempDir(dir)); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ReplaceDir replaces the directory dir with exactly files, by path
// relative to dir (slash-separated; parents created as needed). The files
// are written into a hidden staging sibling of dir, which is then swapped
// in: a leftover from a killed run is skipped by every reader of the tree.
// No file is synced: staging is invisible until the swap, and syncing
// thousands of node files would cost more than the run they render.
//
// Requires: every key of files is a relative path inside dir.
// Ensures:  when a write or the swap fails, dir holds what it held before
// and the staging directory is gone; on success dir holds exactly files,
// and no files at all leaves no dir. An error removing the previous
// content is returned after the new content is in place.
func ReplaceDir(dir string, files map[string][]byte) error {
	if len(files) == 0 {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
		return nil
	}
	parent, base := filepath.Dir(dir), filepath.Base(dir)
	if err := os.MkdirAll(parent, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+base+".new-*")
	if err != nil {
		return fmt.Errorf("create staging dir for %s: %w", dir, err)
	}
	if err = os.Chmod(staging, dirPerm); err == nil {
		err = fill(staging, files)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(staging))
	}

	// The previous content moves aside under a name derived from the
	// staging dir, which is unique, so it collides with nothing.
	aside := staging + ".old"
	hadPrevious := true
	if err = os.Rename(dir, aside); errors.Is(err, fs.ErrNotExist) {
		hadPrevious = false
	} else if err != nil {
		return errors.Join(fmt.Errorf("move %s aside: %w", dir, err), os.RemoveAll(staging))
	}
	if err = os.Rename(staging, dir); err != nil {
		swapErr := fmt.Errorf("move new %s into place: %w", dir, err)
		if hadPrevious {
			return errors.Join(swapErr, os.Rename(aside, dir), os.RemoveAll(staging))
		}
		return errors.Join(swapErr, os.RemoveAll(staging))
	}
	if !hadPrevious {
		return nil
	}
	if err = os.RemoveAll(aside); err != nil {
		return fmt.Errorf("remove previous %s: %w", dir, err)
	}
	return nil
}

// fill writes files under staging, in path order.
func fill(staging string, files map[string][]byte) error {
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		local := filepath.FromSlash(rel)
		if !filepath.IsLocal(local) {
			return fmt.Errorf("write %s: path not inside the directory", rel)
		}
		path := filepath.Join(staging, local)
		if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
			return fmt.Errorf("create dir for %s: %w", rel, err)
		}
		if err := os.WriteFile(path, files[rel], filePerm); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return nil
}
