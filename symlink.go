package switchfs

import (
	"os"
	"syscall"

	"github.com/absfs/absfs"
)

// symlinker returns the backend's symlink support, if it has any.
func symlinker(b absfs.FileSystem) (absfs.SymLinker, bool) {
	l, ok := b.(absfs.SymLinker)
	return l, ok
}

// Lstat returns file information without following a final symlink. The
// path is routed and rewritten like Stat. When the backend has no symlink
// support (doesn't implement absfs.SymLinker), Lstat falls back to Stat.
func (fs *SwitchFS) Lstat(name string) (os.FileInfo, error) {
	vpath, backend, p, err := fs.resolveVirtual("lstat", name)
	if err != nil {
		return nil, err
	}
	info, err := lstat(backend, p)
	if err != nil {
		return nil, pathError(err, name)
	}
	return wrapInfo(info, vpath), nil
}

// Lchown changes the owner and group of name without following a final
// symlink. It fails with an *os.PathError wrapping ErrSymlinkNotSupported
// when the backend has no symlink support.
func (fs *SwitchFS) Lchown(name string, uid, gid int) error {
	backend, p, err := fs.resolve("lchown", name)
	if err != nil {
		return err
	}
	l, ok := symlinker(backend)
	if !ok {
		return &os.PathError{Op: "lchown", Path: name, Err: ErrSymlinkNotSupported}
	}
	return pathError(l.Lchown(p, uid, gid), name)
}

// Readlink returns the target of the symlink name exactly as it was stored;
// targets are never rewritten. It fails with an *os.PathError wrapping
// ErrSymlinkNotSupported when the backend has no symlink support.
func (fs *SwitchFS) Readlink(name string) (string, error) {
	backend, p, err := fs.resolve("readlink", name)
	if err != nil {
		return "", err
	}
	l, ok := symlinker(backend)
	if !ok {
		return "", &os.PathError{Op: "readlink", Path: name, Err: ErrSymlinkNotSupported}
	}
	target, err := l.Readlink(p)
	return target, pathError(err, name)
}

// Symlink creates newname as a symbolic link to oldname. Only newname is
// routed and rewritten: the target oldname is opaque and stored as given.
// A relative target resolves the same way in the virtual tree and in the
// backend; an absolute one is resolved by whoever follows the link (the
// backend, for Stat and Open, or an NFS client), so it names a backend path
// in one case and a client path in the other. Prefer relative targets.
//
// Errors are *os.LinkError values carrying oldname and newname; the error
// wraps ErrSymlinkNotSupported when the backend has no symlink support.
func (fs *SwitchFS) Symlink(oldname, newname string) error {
	if newname == "" {
		return &os.LinkError{Op: "symlink", Old: oldname, New: newname, Err: syscall.ENOENT}
	}
	backend, p, err := fs.resolve("symlink", newname)
	if err != nil {
		return err
	}
	l, ok := symlinker(backend)
	if !ok {
		return &os.LinkError{Op: "symlink", Old: oldname, New: newname, Err: ErrSymlinkNotSupported}
	}
	return linkError(l.Symlink(oldname, p), "symlink", oldname, newname)
}
