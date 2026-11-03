package switchfs

import (
	"io"
	"os"
	"path"

	"github.com/absfs/absfs"
)

// lstat stats p in b without following a final symlink when b supports
// symlinks, and falls back to Stat otherwise.
func lstat(b absfs.FileSystem, p string) (os.FileInfo, error) {
	if l, ok := b.(absfs.SymLinker); ok {
		return l.Lstat(p)
	}
	return b.Stat(p)
}

// crossBackendMove moves oldpath in oldBackend to newpath in newBackend by
// copying and then removing. Both paths are backend paths (already
// rewritten). Symlinks are recreated, not followed; directories are moved
// recursively. A failure part way leaves what was already moved in place.
func crossBackendMove(oldpath, newpath string, oldBackend, newBackend absfs.FileSystem) error {
	info, err := lstat(oldBackend, oldpath)
	if err != nil {
		return err
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return crossBackendMoveSymlink(oldpath, newpath, oldBackend, newBackend)
	case info.IsDir():
		return crossBackendMoveDir(oldpath, newpath, oldBackend, newBackend, info)
	default:
		return crossBackendMoveFile(oldpath, newpath, oldBackend, newBackend, info)
	}
}

// crossBackendMoveSymlink recreates a symlink, with its target unchanged, in
// the new backend and removes the original.
func crossBackendMoveSymlink(oldpath, newpath string, oldBackend, newBackend absfs.FileSystem) error {
	src := oldBackend.(absfs.SymLinker) // lstat only reports symlinks from a SymLinker
	target, err := src.Readlink(oldpath)
	if err != nil {
		return err
	}
	dst, ok := newBackend.(absfs.SymLinker)
	if !ok {
		return &os.PathError{Op: "symlink", Path: newpath, Err: ErrSymlinkNotSupported}
	}
	if err := dst.Symlink(target, newpath); err != nil {
		return err
	}
	return oldBackend.Remove(oldpath)
}

// crossBackendMoveFile copies one regular file, keeping its permission bits
// and modification time where the new backend allows, then removes it.
func crossBackendMoveFile(oldpath, newpath string, oldBackend, newBackend absfs.FileSystem, info os.FileInfo) error {
	src, err := oldBackend.Open(oldpath)
	if err != nil {
		return err
	}

	dst, err := newBackend.OpenFile(newpath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		src.Close()
		return err
	}

	_, err = io.Copy(dst, src)
	src.Close() // closed before Remove, which some hosts require
	if err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}

	// Best effort: not every backend keeps times.
	_ = newBackend.Chtimes(newpath, info.ModTime(), info.ModTime())

	return oldBackend.Remove(oldpath)
}

// crossBackendMoveDir moves a directory tree, then removes the source.
func crossBackendMoveDir(oldpath, newpath string, oldBackend, newBackend absfs.FileSystem, info os.FileInfo) error {
	if err := newBackend.MkdirAll(newpath, info.Mode().Perm()); err != nil {
		return err
	}

	entries, err := oldBackend.ReadDir(oldpath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		if err := crossBackendMove(path.Join(oldpath, name), path.Join(newpath, name), oldBackend, newBackend); err != nil {
			return err
		}
	}

	return oldBackend.RemoveAll(oldpath)
}
