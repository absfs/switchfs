package switchfs

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/absfs/absfs"
)

// SwitchFS implements absfs.FileSystem with routing
type SwitchFS struct {
	router      Router
	defaultFS   absfs.FileSystem
	currentDir  string
	separator   uint8
	listSep     uint8
	tempDir     string
}

// Ensure SwitchFS implements absfs.FileSystem
var _ absfs.FileSystem = (*SwitchFS)(nil)

// New creates a new SwitchFS with the given options
func New(opts ...Option) (*SwitchFS, error) {
	fs := &SwitchFS{
		router:      NewRouter(),
		currentDir:  "/",
		separator:   '/',
		listSep:     ':',
		tempDir:     "/tmp",
	}

	for _, opt := range opts {
		if err := opt(fs); err != nil {
			return nil, err
		}
	}

	return fs, nil
}

// getBackend finds the appropriate backend for a path
func (fs *SwitchFS) getBackend(path string) (absfs.FileSystem, error) {
	// Try to route the path
	backend, err := fs.router.Route(path)
	if err == ErrNoRoute {
		// Use default backend if no route matches
		if fs.defaultFS != nil {
			return fs.defaultFS, nil
		}
		return nil, ErrNoRoute
	}
	return backend, err
}

// Separator returns the path separator
func (fs *SwitchFS) Separator() uint8 {
	return fs.separator
}

// ListSeparator returns the list separator
func (fs *SwitchFS) ListSeparator() uint8 {
	return fs.listSep
}

// Chdir changes the current working directory
func (fs *SwitchFS) Chdir(dir string) error {
	// Normalize the path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(fs.currentDir, dir)
	}
	fs.currentDir = filepath.Clean(dir)
	return nil
}

// Getwd returns the current working directory
func (fs *SwitchFS) Getwd() (string, error) {
	return fs.currentDir, nil
}

// TempDir returns the temporary directory
func (fs *SwitchFS) TempDir() string {
	return fs.tempDir
}

// OpenFile opens a file with the specified flags and permissions
func (fs *SwitchFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	backend, err := fs.getBackend(name)
	if err != nil {
		return nil, err
	}
	return backend.OpenFile(name, flag, perm)
}

// Open opens a file for reading
func (fs *SwitchFS) Open(name string) (absfs.File, error) {
	return fs.OpenFile(name, os.O_RDONLY, 0)
}

// Create creates a new file
func (fs *SwitchFS) Create(name string) (absfs.File, error) {
	return fs.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// Mkdir creates a directory
func (fs *SwitchFS) Mkdir(name string, perm os.FileMode) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Mkdir(name, perm)
}

// MkdirAll creates a directory and all parent directories
func (fs *SwitchFS) MkdirAll(name string, perm os.FileMode) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.MkdirAll(name, perm)
}

// Remove removes a file or empty directory
func (fs *SwitchFS) Remove(name string) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Remove(name)
}

// RemoveAll removes a path and all children
func (fs *SwitchFS) RemoveAll(path string) error {
	backend, err := fs.getBackend(path)
	if err != nil {
		return err
	}
	return backend.RemoveAll(path)
}

// Rename renames (moves) oldpath to newpath
func (fs *SwitchFS) Rename(oldpath, newpath string) error {
	oldBackend, err := fs.getBackend(oldpath)
	if err != nil {
		return err
	}

	newBackend, err := fs.getBackend(newpath)
	if err != nil {
		return err
	}

	// If both paths are on the same backend, use native rename
	if oldBackend == newBackend {
		return oldBackend.Rename(oldpath, newpath)
	}

	// Cross-backend rename: copy then delete
	return fs.crossBackendMove(oldpath, newpath, oldBackend, newBackend)
}

// crossBackendMove handles moving files across different backends
func (fs *SwitchFS) crossBackendMove(oldpath, newpath string, oldBackend, newBackend absfs.FileSystem) error {
	// Get file info
	info, err := oldBackend.Stat(oldpath)
	if err != nil {
		return err
	}

	// Handle directories
	if info.IsDir() {
		return ErrCrossBackendOperation
	}

	// Open source file
	src, err := oldBackend.Open(oldpath)
	if err != nil {
		return err
	}
	defer src.Close()

	// Create destination file
	dst, err := newBackend.Create(newpath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Copy data
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	// Close destination to flush
	if err := dst.Close(); err != nil {
		return err
	}

	// Remove source
	return oldBackend.Remove(oldpath)
}

// Stat returns file information
func (fs *SwitchFS) Stat(name string) (os.FileInfo, error) {
	backend, err := fs.getBackend(name)
	if err != nil {
		return nil, err
	}
	return backend.Stat(name)
}

// Chmod changes file permissions
func (fs *SwitchFS) Chmod(name string, mode os.FileMode) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Chmod(name, mode)
}

// Chtimes changes file access and modification times
func (fs *SwitchFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Chtimes(name, atime, mtime)
}

// Chown changes file owner and group
func (fs *SwitchFS) Chown(name string, uid, gid int) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Chown(name, uid, gid)
}

// Truncate changes the size of a file
func (fs *SwitchFS) Truncate(name string, size int64) error {
	backend, err := fs.getBackend(name)
	if err != nil {
		return err
	}
	return backend.Truncate(name, size)
}

// Router returns the underlying router for advanced usage
func (fs *SwitchFS) Router() Router {
	return fs.router
}
