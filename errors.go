package switchfs

import "errors"

var (
	// ErrNoRoute is returned when no route matches the given path
	ErrNoRoute = errors.New("no route found for path")

	// ErrAllBackendsFailed is returned when both primary and failover backends fail
	ErrAllBackendsFailed = errors.New("all backends failed")

	// ErrCrossBackendOperation is returned for unsupported operations that span multiple backends
	ErrCrossBackendOperation = errors.New("operation spans multiple backends")

	// ErrInvalidPattern is returned when a route pattern is invalid
	ErrInvalidPattern = errors.New("invalid route pattern")

	// ErrDuplicateRoute is returned when attempting to add a route with an existing pattern
	ErrDuplicateRoute = errors.New("route with pattern already exists")

	// ErrNilBackend is returned when a nil backend is provided
	ErrNilBackend = errors.New("backend cannot be nil")
)
