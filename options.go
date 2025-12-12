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

// WithRoute adds a routing rule
func WithRoute(pattern string, backend absfs.FileSystem, opts ...RouteOption) Option {
	return func(fs *SwitchFS) error {
		if backend == nil {
			return ErrNilBackend
		}

		route := Route{
			Pattern:  pattern,
			Backend:  backend,
			Priority: 0,
			Type:     PatternPrefix,
		}

		// Apply route options
		for _, opt := range opts {
			if err := opt(&route); err != nil {
				return err
			}
		}

		return fs.router.AddRoute(route)
	}
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

// WithFailover sets a failover backend
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

// WithRouter sets a custom router implementation
func WithRouter(router Router) Option {
	return func(fs *SwitchFS) error {
		if router == nil {
			return ErrNilBackend
		}
		fs.router = router
		return nil
	}
}
