package switchfs

import (
	"time"

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

// WithSeparator sets the path separator
func WithSeparator(sep uint8) Option {
	return func(fs *SwitchFS) error {
		fs.separator = sep
		return nil
	}
}

// WithListSeparator sets the list separator
func WithListSeparator(sep uint8) Option {
	return func(fs *SwitchFS) error {
		fs.listSep = sep
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

// WithCache enables route caching with the specified max size and TTL
func WithCache(maxSize int, ttl time.Duration) Option {
	return func(fs *SwitchFS) error {
		fs.router = NewRouterWithCache(maxSize, ttl)
		return nil
	}
}

// WithHealthMonitoring enables backend health monitoring and circuit breakers
func WithHealthMonitoring(failureThreshold int, circuitTimeout, recoveryTimeout time.Duration) Option {
	return func(fs *SwitchFS) error {
		fs.healthMonitor = NewHealthMonitor(failureThreshold, circuitTimeout, recoveryTimeout)
		return nil
	}
}

// WithRetry enables retry logic with exponential backoff
func WithRetry(config *RetryConfig) Option {
	return func(fs *SwitchFS) error {
		fs.retryConfig = config
		return nil
	}
}

// WithDefaultRetry enables retry logic with default configuration
func WithDefaultRetry() Option {
	return func(fs *SwitchFS) error {
		fs.retryConfig = DefaultRetryConfig()
		return nil
	}
}

// WithMiddleware adds a middleware to the filesystem
func WithMiddleware(middleware Middleware) Option {
	return func(fs *SwitchFS) error {
		fs.middleware = append(fs.middleware, middleware)
		return nil
	}
}

// WithStats enables statistics collection
func WithStats(collector *StatsCollector) Option {
	return func(fs *SwitchFS) error {
		fs.stats = collector
		// Also add stats middleware
		fs.middleware = append(fs.middleware, NewStatsMiddleware(collector))
		return nil
	}
}
