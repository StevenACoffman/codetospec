// Package atomicfs writes files and directories so that neither a reader
// nor a crash part way through ever sees them half written: the new content
// is built beside the target, then renamed over it.
package atomicfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// dirPerm is the mode of the directories ReplaceDir creates, the mode the
// output tree has always had.
const dirPerm = 0o750

// WriteFile writes data to path via a temporary file and rename, so a
// crash never leaves a truncated file behind. A failure to remove the
// temporary file after an error is reported with it.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	if written := errors.Join(writeErr, tmp.Close()); written != nil {
		return errors.Join(fmt.Errorf("write %s: %w", tmpName, written), os.Remove(tmpName))
	}
	if renameErr := os.Rename(tmpName, path); renameErr != nil {
		return errors.Join(fmt.Errorf("rename to %s: %w", path, renameErr), os.Remove(tmpName))
	}
	return nil
}

// ReplaceDir replaces the directory dir with what fill writes into the
// empty staging directory it is given. The staging directory is a hidden
// sibling of dir, so a leftover from a killed run is skipped by every
// reader of the tree.
//
// Requires: fill writes only under its argument.
// Ensures:  when fill or the swap fails, dir holds what it held before
// and the staging directory is gone; on success dir holds exactly fill's
// output. An error removing the previous content is returned after the new
// content is in place.
func ReplaceDir(dir string, fill func(staging string) error) error {
	parent, base := filepath.Dir(dir), filepath.Base(dir)
	if err := os.MkdirAll(parent, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+base+".new-*")
	if err != nil {
		return fmt.Errorf("create staging dir for %s: %w", dir, err)
	}
	if err = os.Chmod(staging, dirPerm); err != nil {
		return errors.Join(fmt.Errorf("chmod %s: %w", staging, err), os.RemoveAll(staging))
	}
	if err = fill(staging); err != nil {
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
