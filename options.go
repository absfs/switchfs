package switchfs

import (
	"github.com/absfs/absfs"
)

// WithDefault sets the default backend for unmatched paths
func WithDefault(backend absfs.FileSystem) Option {
	return func(fs *SwitchFS) error {
		if backend == nil {
			return ErrNilBackend
		}
		fs.defaultFS = backend
		return nil
	}
}

// WithRoute adds a routing rule. The pattern is a prefix unless
// WithPatternType says otherwise; priority defaults to 0.
func WithRoute(pattern string, backend absfs.FileSystem, opts ...RouteOption) Option {
	return func(fs *SwitchFS) error {
		route, err := newRoute(pattern, backend, opts...)
		if err != nil {
			return err
		}
		return fs.router.AddRoute(route)
	}
}

// newRoute builds a prefix route with priority 0 and applies opts.
func newRoute(pattern string, backend absfs.FileSystem, opts ...RouteOption) (Route, error) {
	if backend == nil {
		return Route{}, ErrNilBackend
	}

	route := Route{
		Pattern:  pattern,
		Backend:  backend,
		Priority: 0,
		Type:     PatternPrefix,
	}

	for _, opt := range opts {
		if err := opt(&route); err != nil {
			return Route{}, err
		}
	}
	return route, nil
}

// WithPriority sets route priority
func WithPriority(priority int) RouteOption {
	return func(r *Route) error {
		r.Priority = priority
		return nil
	}
}

// WithPatternType sets the pattern matching type
func WithPatternType(pt PatternType) RouteOption {
	return func(r *Route) error {
		r.Type = pt
		return nil
	}
}

// WithFailover sets a failover backend. It is recorded on the route but
// not yet used by SwitchFS.
func WithFailover(backend absfs.FileSystem) RouteOption {
	return func(r *Route) error {
		if backend == nil {
			return ErrNilBackend
		}
		r.Failover = backend
		return nil
	}
}

// WithCondition sets a condition that must be met for routing
func WithCondition(condition RouteCondition) RouteOption {
	return func(r *Route) error {
		r.Condition = condition
		return nil
	}
}

// WithRewriter sets a path rewriter for the route
func WithRewriter(rewriter PathRewriter) RouteOption {
	return func(r *Route) error {
		r.Rewriter = rewriter
		return nil
	}
}

// WithTempDir sets the temporary directory path
func WithTempDir(dir string) Option {
	return func(fs *SwitchFS) error {
		fs.tempDir = dir
		return nil
	}
}

// WithRouter sets a custom router implementation. A custom router should
// be safe for concurrent use if routes change while the filesystem serves.
func WithRouter(router Router) Option {
	return func(fs *SwitchFS) error {
		if router == nil {
			return ErrNilBackend
		}
		fs.router = router
		return nil
	}
}
