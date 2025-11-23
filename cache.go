package switchfs

import (
	"sync"
	"time"
)

// CacheEntry stores a cached routing decision
type CacheEntry struct {
	RouteIndex int
	Timestamp  time.Time
}

// RouteCache caches routing decisions to improve performance
type RouteCache struct {
	mu      sync.RWMutex
	cache   map[string]*CacheEntry
	maxSize int
	ttl     time.Duration
}

// NewRouteCache creates a new route cache
func NewRouteCache(maxSize int, ttl time.Duration) *RouteCache {
	return &RouteCache{
		cache:   make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get retrieves a cached routing decision
func (c *RouteCache) Get(path string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.cache[path]
	if !ok {
		return -1, false
	}

	// Check if entry has expired
	if c.ttl > 0 && time.Since(entry.Timestamp) > c.ttl {
		return -1, false
	}

	return entry.RouteIndex, true
}

// Set stores a routing decision in the cache
func (c *RouteCache) Set(path string, routeIndex int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entry if cache is full
	if len(c.cache) >= c.maxSize {
		c.evictOldest()
	}

	c.cache[path] = &CacheEntry{
		RouteIndex: routeIndex,
		Timestamp:  time.Now(),
	}
}

// evictOldest removes the oldest cache entry
func (c *RouteCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	first := true
	for key, entry := range c.cache {
		if first || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
			first = false
		}
	}

	if oldestKey != "" {
		delete(c.cache, oldestKey)
	}
}

// Clear removes all entries from the cache
func (c *RouteCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*CacheEntry)
}

// Size returns the current number of cached entries
func (c *RouteCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Enable enables caching
func (c *RouteCache) Enable() {
	// Cache is always enabled once created
}

// Disable disables caching by clearing the cache
func (c *RouteCache) Disable() {
	c.Clear()
}
