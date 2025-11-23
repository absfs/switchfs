package switchfs

import (
	"os"
	"sort"
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// Router manages routing decisions
type Router interface {
	// AddRoute adds a routing rule
	AddRoute(route Route) error

	// RemoveRoute removes a routing rule by pattern
	RemoveRoute(pattern string) error

	// Route finds the backend for a given path
	Route(path string) (absfs.FileSystem, error)

	// RouteWithInfo finds the route for a given path with file info for condition evaluation
	RouteWithInfo(path string, info os.FileInfo) (*Route, error)

	// Routes returns all registered routes
	Routes() []Route
}

// router is the default implementation of Router
type router struct {
	mu     sync.RWMutex
	routes []Route
	cache  *RouteCache
}

// NewRouter creates a new router instance
func NewRouter() Router {
	return &router{
		routes: make([]Route, 0),
		cache:  nil, // Caching disabled by default
	}
}

// NewRouterWithCache creates a new router instance with caching enabled
func NewRouterWithCache(maxCacheSize int, cacheTTL time.Duration) Router {
	return &router{
		routes: make([]Route, 0),
		cache:  NewRouteCache(maxCacheSize, cacheTTL),
	}
}

// AddRoute adds a routing rule
func (r *router) AddRoute(route Route) error {
	if route.Backend == nil {
		return ErrNilBackend
	}

	// Compile the pattern matcher
	matcher, err := compileMatcher(route.Pattern, route.Type)
	if err != nil {
		return err
	}
	route.compiled = matcher

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicate patterns
	for _, existing := range r.routes {
		if existing.Pattern == route.Pattern && existing.Type == route.Type {
			return ErrDuplicateRoute
		}
	}

	// Add the route
	r.routes = append(r.routes, route)

	// Sort routes by priority (highest first)
	sort.Slice(r.routes, func(i, j int) bool {
		return r.routes[i].Priority > r.routes[j].Priority
	})

	// Invalidate cache since routes changed
	if r.cache != nil {
		r.cache.Clear()
	}

	return nil
}

// RemoveRoute removes a routing rule by pattern
func (r *router) RemoveRoute(pattern string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, route := range r.routes {
		if route.Pattern == pattern {
			// Remove the route
			r.routes = append(r.routes[:i], r.routes[i+1:]...)

			// Invalidate cache since routes changed
			if r.cache != nil {
				r.cache.Clear()
			}

			return nil
		}
	}

	return ErrNoRoute
}

// Route finds the backend for a given path
func (r *router) Route(path string) (absfs.FileSystem, error) {
	// Check cache first
	if r.cache != nil {
		if idx, ok := r.cache.Get(path); ok {
			r.mu.RLock()
			if idx >= 0 && idx < len(r.routes) {
				backend := r.routes[idx].Backend
				r.mu.RUnlock()
				return backend, nil
			}
			r.mu.RUnlock()
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Iterate through routes in priority order
	for i, route := range r.routes {
		if route.compiled != nil && route.compiled.Match(path) {
			// Cache the result
			if r.cache != nil {
				r.cache.Set(path, i)
			}
			return route.Backend, nil
		}
	}

	return nil, ErrNoRoute
}

// RouteWithInfo finds the route for a given path with file info for condition evaluation
func (r *router) RouteWithInfo(path string, info os.FileInfo) (*Route, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Iterate through routes in priority order
	for i := range r.routes {
		route := &r.routes[i]

		// Check if pattern matches
		if route.compiled == nil || !route.compiled.Match(path) {
			continue
		}

		// Check condition if present
		if route.Condition != nil && !route.Condition.Evaluate(path, info) {
			continue
		}

		return route, nil
	}

	return nil, ErrNoRoute
}

// Routes returns all registered routes
func (r *router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	routes := make([]Route, len(r.routes))
	copy(routes, r.routes)
	return routes
}
