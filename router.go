package switchfs

import (
	"os"
	"sort"
	"sync"

	"github.com/absfs/absfs"
)

// Router manages routing decisions.
//
// The default implementation returned by NewRouter is safe for concurrent
// use: routes may be added and removed while other goroutines route paths.
type Router interface {
	// AddRoute adds a routing rule
	AddRoute(route Route) error

	// RemoveRoute removes a routing rule by pattern. The default router
	// removes the highest-priority route with that pattern, whatever its
	// Type; use TypedRouteRemover (or SwitchFS.RemoveRoute) to name the
	// type as well.
	RemoveRoute(pattern string) error

	// Route finds the backend for a given path, ignoring route conditions
	Route(path string) (absfs.FileSystem, error)

	// RouteWithInfo finds the route for a given path with file info for
	// condition evaluation. SwitchFS calls it for every operation, with a
	// nil info.
	RouteWithInfo(path string, info os.FileInfo) (*Route, error)

	// Routes returns all registered routes
	Routes() []Route
}

// TypedRouteRemover is implemented by routers that can remove a route by its
// pattern and its pattern type together. The default router implements it.
type TypedRouteRemover interface {
	// RemoveRouteOfType removes the route whose Pattern and Type both match,
	// returning ErrNoRoute when there is none.
	RemoveRouteOfType(pattern string, typ PatternType) error
}

// router is the default implementation of Router
type router struct {
	mu     sync.RWMutex
	routes []Route
}

var _ TypedRouteRemover = (*router)(nil)

// NewRouter creates a new router instance
func NewRouter() Router {
	return &router{
		routes: make([]Route, 0),
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

	// Build a new slice so routes handed out earlier are never mutated.
	routes := make([]Route, len(r.routes), len(r.routes)+1)
	copy(routes, r.routes)
	routes = append(routes, route)

	// Sort routes by priority (highest first); equal priorities keep the
	// order they were added in.
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Priority > routes[j].Priority
	})
	r.routes = routes

	return nil
}

// RemoveRoute removes the highest-priority routing rule with the pattern
func (r *router) RemoveRoute(pattern string) error {
	return r.remove(func(rt *Route) bool { return rt.Pattern == pattern })
}

// RemoveRouteOfType removes the routing rule with the pattern and type
func (r *router) RemoveRouteOfType(pattern string, typ PatternType) error {
	return r.remove(func(rt *Route) bool { return rt.Pattern == pattern && rt.Type == typ })
}

func (r *router) remove(match func(*Route) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.routes {
		if !match(&r.routes[i]) {
			continue
		}
		routes := make([]Route, 0, len(r.routes)-1)
		routes = append(routes, r.routes[:i]...)
		routes = append(routes, r.routes[i+1:]...)
		r.routes = routes
		return nil
	}

	return ErrNoRoute
}

// Route finds the backend for a given path
func (r *router) Route(path string) (absfs.FileSystem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Iterate through routes in priority order
	for _, route := range r.routes {
		if route.compiled != nil && route.compiled.Match(path) {
			return route.Backend, nil
		}
	}

	return nil, ErrNoRoute
}

// RouteWithInfo finds the route for a given path with file info for
// condition evaluation. The returned Route is a copy owned by the caller.
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

		found := *route
		return &found, nil
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
