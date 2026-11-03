package switchfs

import (
	"errors"
	"fmt"
)

var (
	// ErrNoRoute is returned when no route matches the given path and there
	// is no default backend. Operations return it unwrapped, so callers may
	// compare with ==.
	ErrNoRoute = errors.New("no route found for path")

	// ErrAllBackendsFailed is returned when both primary and failover backends fail
	ErrAllBackendsFailed = errors.New("all backends failed")

	// ErrCrossBackendOperation is returned for unsupported operations that span multiple backends
	ErrCrossBackendOperation = errors.New("operation spans multiple backends")

	// ErrInvalidPattern is returned when a route pattern is invalid
	ErrInvalidPattern = errors.New("invalid route pattern")

	// ErrDuplicateRoute is returned when attempting to add a route with an
	// existing pattern and pattern type
	ErrDuplicateRoute = errors.New("route with pattern already exists")

	// ErrNilBackend is returned when a nil backend (or nil router) is provided
	ErrNilBackend = errors.New("backend cannot be nil")

	// ErrSymlinkNotSupported is wrapped in the error returned by Lchown,
	// Readlink and Symlink when the path routes to a backend that doesn't
	// implement absfs.SymLinker. It matches errors.ErrUnsupported.
	ErrSymlinkNotSupported = fmt.Errorf("switchfs: backend does not support symlinks: %w", errors.ErrUnsupported)
)
