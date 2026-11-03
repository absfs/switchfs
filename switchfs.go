package switchfs

import (
	"io/fs"
	"os"
	"path"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/absfs/absfs"
)

// SwitchFS is an absfs.SymlinkFileSystem that routes every operation to a
// backend filesystem chosen by the path. See the package documentation for
// how paths are resolved, rewritten and reported back to callers.
//
// A SwitchFS is safe for concurrent use. Routes may be added and removed
// while it is serving; each operation routes against the routes present
// when it starts.
type SwitchFS struct {
	router    Router
	defaultFS absfs.FileSystem
	tempDir   string

	mu         sync.RWMutex // guards currentDir
	currentDir string
}

// Ensure SwitchFS implements absfs.FileSystem and absfs.SymlinkFileSystem.
var (
	_ absfs.FileSystem        = (*SwitchFS)(nil)
	_ absfs.SymlinkFileSystem = (*SwitchFS)(nil)
)

// New creates a new SwitchFS with the given options.
func New(opts ...Option) (*SwitchFS, error) {
	fs := &SwitchFS{
		router:     NewRouter(),
		currentDir: "/",
		tempDir:    "/tmp",
	}

	for _, opt := range opts {
		if err := opt(fs); err != nil {
			return nil, err
		}
	}

	return fs, nil
}

// abs resolves name against the working directory and cleans it, giving the
// virtual path that is matched against the routes.
func (fs *SwitchFS) abs(name string) string {
	if path.IsAbs(name) {
		return path.Clean(name)
	}
	fs.mu.RLock()
	cwd := fs.currentDir
	fs.mu.RUnlock()
	return path.Join(cwd, name)
}

// getBackend finds the backend for a virtual path. It applies the same
// routing as every operation but discards the rewritten path.
func (fs *SwitchFS) getBackend(name string) (absfs.FileSystem, error) {
	backend, _, err := fs.getBackendAndRewrite(name, nil)
	return backend, err
}

// getBackendAndRewrite finds the backend for a virtual path and the path to
// hand that backend. The route's Rewriter, if any, is applied, and a rewrite
// that yields "" becomes "/". Paths that match no route go unchanged to the
// default backend, or fail with ErrNoRoute when there is none.
func (fs *SwitchFS) getBackendAndRewrite(name string, info os.FileInfo) (absfs.FileSystem, string, error) {
	route, err := fs.router.RouteWithInfo(name, info)
	if err == ErrNoRoute {
		if fs.defaultFS != nil {
			return fs.defaultFS, name, nil
		}
		return nil, name, ErrNoRoute
	}
	if err != nil {
		return nil, name, err
	}

	rewritten := name
	if route.Rewriter != nil {
		rewritten = route.Rewriter.Rewrite(name)
		if rewritten == "" {
			rewritten = "/"
		}
	}

	return route.Backend, rewritten, nil
}

// resolve turns a caller's name into a backend and a backend path.
func (fs *SwitchFS) resolve(name string) (absfs.FileSystem, string, error) {
	return fs.getBackendAndRewrite(fs.abs(name), nil)
}

// sameBackend reports whether a and b are the same filesystem. Backends whose
// dynamic type isn't comparable are never considered the same.
func sameBackend(a, b absfs.FileSystem) bool {
	if a == nil || b == nil {
		return false
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}

// Chdir changes the working directory used to resolve relative paths. The
// directory is resolved, routed and checked like any other path: it must
// exist and be a directory. The working directory is virtual; backends'
// own working directories are never changed.
func (fs *SwitchFS) Chdir(dir string) error {
	v := fs.abs(dir)
	backend, p, err := fs.getBackendAndRewrite(v, nil)
	if err != nil {
		return err
	}
	info, err := backend.Stat(p)
	if err != nil {
		if pe, ok := err.(*os.PathError); ok {
			return &os.PathError{Op: "chdir", Path: dir, Err: pe.Err}
		}
		return err
	}
	if !info.IsDir() {
		return &os.PathError{Op: "chdir", Path: dir, Err: syscall.ENOTDIR}
	}

	fs.mu.Lock()
	fs.currentDir = v
	fs.mu.Unlock()
	return nil
}

// Getwd returns the virtual working directory.
func (fs *SwitchFS) Getwd() (string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.currentDir, nil
}

// TempDir returns the virtual temporary directory (default "/tmp", see
// WithTempDir). It is routed like any other path.
func (fs *SwitchFS) TempDir() string {
	return fs.tempDir
}

// OpenFile opens a file with the specified flags and permissions. The
// returned file's Name is the name passed in, not the backend path.
func (fs *SwitchFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := backend.OpenFile(p, flag, perm)
	if err != nil {
		return nil, pathError(err, name)
	}
	return wrapFile(f, name, fs.abs(name)), nil
}

// Open opens a file for reading.
func (fs *SwitchFS) Open(name string) (absfs.File, error) {
	return fs.OpenFile(name, os.O_RDONLY, 0)
}

// Create creates or truncates a file for reading and writing.
func (fs *SwitchFS) Create(name string) (absfs.File, error) {
	return fs.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// Mkdir creates a directory.
func (fs *SwitchFS) Mkdir(name string, perm os.FileMode) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Mkdir(p, perm), name)
}

// MkdirAll creates a directory and any missing parents. Parents are created
// in the backend the full path routes to, under its rewritten path.
func (fs *SwitchFS) MkdirAll(name string, perm os.FileMode) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.MkdirAll(p, perm), name)
}

// Remove removes a file or empty directory.
func (fs *SwitchFS) Remove(name string) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Remove(p), name)
}

// RemoveAll removes a path and any children it contains, in the backend the
// path routes to. Removing a route's own root (for example "/ws" routed with
// StripPrefix("/ws")) removes the backend root's contents, as the backend
// does for RemoveAll("/").
func (fs *SwitchFS) RemoveAll(name string) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.RemoveAll(p), name)
}

// Rename renames (moves) oldpath to newpath. When both paths route to the
// same backend the backend's own Rename is used with both rewritten paths;
// otherwise the tree is copied to the new backend and removed from the old
// one, which is not atomic. Errors are *os.LinkError values carrying the
// caller's paths.
func (fs *SwitchFS) Rename(oldpath, newpath string) error {
	oldBackend, op, err := fs.resolve(oldpath)
	if err != nil {
		return err
	}
	newBackend, np, err := fs.resolve(newpath)
	if err != nil {
		return err
	}

	if sameBackend(oldBackend, newBackend) {
		return linkError(oldBackend.Rename(op, np), "rename", oldpath, newpath)
	}
	return linkError(crossBackendMove(op, np, oldBackend, newBackend), "rename", oldpath, newpath)
}

// Stat returns file information, following symlinks inside the backend.
func (fs *SwitchFS) Stat(name string) (os.FileInfo, error) {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	info, err := backend.Stat(p)
	if err != nil {
		return nil, pathError(err, name)
	}
	return wrapInfo(info, fs.abs(name)), nil
}

// Chmod changes file permissions.
func (fs *SwitchFS) Chmod(name string, mode os.FileMode) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Chmod(p, mode), name)
}

// Chtimes changes file access and modification times.
func (fs *SwitchFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Chtimes(p, atime, mtime), name)
}

// Chown changes file owner and group.
func (fs *SwitchFS) Chown(name string, uid, gid int) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Chown(p, uid, gid), name)
}

// Truncate changes the size of a file.
func (fs *SwitchFS) Truncate(name string, size int64) error {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return pathError(backend.Truncate(p, size), name)
}

// Router returns the underlying router for advanced usage, such as adding
// and removing routes while the filesystem is serving.
func (fs *SwitchFS) Router() Router {
	return fs.router
}

// ReadDir reads the named directory and returns its entries. Only the
// backend the directory routes to is listed: routes below the directory are
// not merged in (see the package documentation for listing route parents).
func (fs *SwitchFS) ReadDir(name string) ([]fs.DirEntry, error) {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	entries, err := backend.ReadDir(p)
	if err != nil {
		return entries, pathError(err, name)
	}
	return entries, nil
}

// ReadFile reads the named file and returns its contents.
func (fs *SwitchFS) ReadFile(name string) ([]byte, error) {
	backend, p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	data, err := backend.ReadFile(p)
	if err != nil {
		return data, pathError(err, name)
	}
	return data, nil
}

// Sub returns a read-only fs.FS rooted at dir. The subtree is served through
// this SwitchFS, so paths below dir keep routing (and rewriting) even when
// they cross into other routes.
func (fs *SwitchFS) Sub(dir string) (fs.FS, error) {
	return absfs.FilerToFS(fs, fs.abs(dir))
}

// AddRoute adds a route while the filesystem is in use. It takes the same
// options as WithRoute and is safe to call concurrently with any operation;
// operations that start after it returns see the new route.
func (fs *SwitchFS) AddRoute(pattern string, backend absfs.FileSystem, opts ...RouteOption) error {
	route, err := newRoute(pattern, backend, opts...)
	if err != nil {
		return err
	}
	return fs.router.AddRoute(route)
}

// RemoveRoute removes the route with the given pattern and pattern type
// while the filesystem is in use. Unlike Router.RemoveRoute, which matches
// the pattern alone, it leaves routes with the same pattern but another type
// in place. Files already open through the removed route stay usable.
//
// With a custom Router that doesn't implement TypedRouteRemover, it checks
// Routes for a matching (pattern, type) and then falls back to
// Router.RemoveRoute(pattern).
func (fs *SwitchFS) RemoveRoute(pattern string, typ PatternType) error {
	if r, ok := fs.router.(TypedRouteRemover); ok {
		return r.RemoveRouteOfType(pattern, typ)
	}
	for _, rt := range fs.router.Routes() {
		if rt.Pattern == pattern && rt.Type == typ {
			return fs.router.RemoveRoute(pattern)
		}
	}
	return ErrNoRoute
}
