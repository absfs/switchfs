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

	// ErrAllBackendsFailed is reserved for failover (both the primary and
	// the failover backend failed). Failover is not implemented, so nothing
	// in this package returns it yet.
	ErrAllBackendsFailed = errors.New("all backends failed")

	// ErrCrossBackendOperation is reserved for operations that can't span
	// backends. Nothing in this package returns it: Rename across backends
	// copies and removes instead.
	ErrCrossBackendOperation = errors.New("operation spans multiple backends")

	// ErrInvalidPattern is returned when a route pattern is invalid
	ErrInvalidPattern = errors.New("invalid route pattern")

	// ErrDuplicateRoute is returned when attempting to add a route with an
	// existing pattern and pattern type
	ErrDuplicateRoute = errors.New("route with pattern already exists")

	// ErrAmbiguousRoute is returned by SwitchFS.RemoveRoute when the router
	// can't remove a route by type (it doesn't implement TypedRouteRemover)
	// and routes of more than one type share the pattern, so removing by
	// pattern alone might take the wrong one. Nothing is removed.
	ErrAmbiguousRoute = errors.New("route pattern is shared by routes of other types")

	// ErrNilBackend is returned when a nil backend (or nil router) is provided
	ErrNilBackend = errors.New("backend cannot be nil")

	// ErrSymlinkNotSupported is wrapped in the error returned by Lchown,
	// Readlink and Symlink when the path routes to a backend that doesn't
	// implement absfs.SymLinker. It matches errors.ErrUnsupported.
	ErrSymlinkNotSupported = fmt.Errorf("switchfs: backend does not support symlinks: %w", errors.ErrUnsupported)
)
