package switchfs

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/absfs/absfs"
)

// OperationStats tracks statistics for filesystem operations
type OperationStats struct {
	Count         uint64
	Errors        uint64
	TotalDuration time.Duration
	LastOperation time.Time
}

// RouteStats tracks statistics for a specific route
type RouteStats struct {
	mu         sync.RWMutex
	Pattern    string
	Operations map[OperationType]*OperationStats
	HitCount   uint64
	BytesRead  uint64
	BytesWrite uint64
}

// StatsCollector collects routing and operation statistics
type StatsCollector struct {
	mu             sync.RWMutex
	routes         map[string]*RouteStats
	backends       map[absfs.FileSystem]*RouteStats
	totalOps       uint64
	cacheHits      uint64
	cacheMisses    uint64
	failoverCount  uint64
}

// NewStatsCollector creates a new statistics collector
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		routes:   make(map[string]*RouteStats),
		backends: make(map[absfs.FileSystem]*RouteStats),
	}
}

// RecordOperation records an operation
func (sc *StatsCollector) RecordOperation(pattern string, op OperationType, duration time.Duration, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	stats, ok := sc.routes[pattern]
	if !ok {
		stats = &RouteStats{
			Pattern:    pattern,
			Operations: make(map[OperationType]*OperationStats),
		}
		sc.routes[pattern] = stats
	}

	stats.mu.Lock()
	defer stats.mu.Unlock()

	opStats, ok := stats.Operations[op]
	if !ok {
		opStats = &OperationStats{}
		stats.Operations[op] = opStats
	}

	atomic.AddUint64(&opStats.Count, 1)
	opStats.TotalDuration += duration
	opStats.LastOperation = time.Now()

	if err != nil {
		atomic.AddUint64(&opStats.Errors, 1)
	}

	atomic.AddUint64(&sc.totalOps, 1)
}

// RecordCacheHit records a cache hit
func (sc *StatsCollector) RecordCacheHit() {
	atomic.AddUint64(&sc.cacheHits, 1)
}

// RecordCacheMiss records a cache miss
func (sc *StatsCollector) RecordCacheMiss() {
	atomic.AddUint64(&sc.cacheMisses, 1)
}

// RecordFailover records a failover event
func (sc *StatsCollector) RecordFailover() {
	atomic.AddUint64(&sc.failoverCount, 1)
}

// RecordRouteHit records a hit on a specific route
func (sc *StatsCollector) RecordRouteHit(pattern string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	stats, ok := sc.routes[pattern]
	if !ok {
		stats = &RouteStats{
			Pattern:    pattern,
			Operations: make(map[OperationType]*OperationStats),
		}
		sc.routes[pattern] = stats
	}

	atomic.AddUint64(&stats.HitCount, 1)
}

// GetRouteStats returns statistics for a specific route
func (sc *StatsCollector) GetRouteStats(pattern string) *RouteStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	stats, ok := sc.routes[pattern]
	if !ok {
		return nil
	}

	// Return a copy
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	copy := &RouteStats{
		Pattern:    stats.Pattern,
		Operations: make(map[OperationType]*OperationStats),
		HitCount:   atomic.LoadUint64(&stats.HitCount),
		BytesRead:  atomic.LoadUint64(&stats.BytesRead),
		BytesWrite: atomic.LoadUint64(&stats.BytesWrite),
	}

	for op, opStats := range stats.Operations {
		copy.Operations[op] = &OperationStats{
			Count:         atomic.LoadUint64(&opStats.Count),
			Errors:        atomic.LoadUint64(&opStats.Errors),
			TotalDuration: opStats.TotalDuration,
			LastOperation: opStats.LastOperation,
		}
	}

	return copy
}

// GetAllStats returns all route statistics
func (sc *StatsCollector) GetAllStats() map[string]*RouteStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := make(map[string]*RouteStats)
	for pattern := range sc.routes {
		result[pattern] = sc.GetRouteStats(pattern)
	}

	return result
}

// GetTotalOperations returns the total number of operations
func (sc *StatsCollector) GetTotalOperations() uint64 {
	return atomic.LoadUint64(&sc.totalOps)
}

// GetCacheStats returns cache hit/miss statistics
func (sc *StatsCollector) GetCacheStats() (hits, misses uint64) {
	return atomic.LoadUint64(&sc.cacheHits), atomic.LoadUint64(&sc.cacheMisses)
}

// GetFailoverCount returns the number of failover events
func (sc *StatsCollector) GetFailoverCount() uint64 {
	return atomic.LoadUint64(&sc.failoverCount)
}

// Reset resets all statistics
func (sc *StatsCollector) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.routes = make(map[string]*RouteStats)
	sc.backends = make(map[absfs.FileSystem]*RouteStats)
	atomic.StoreUint64(&sc.totalOps, 0)
	atomic.StoreUint64(&sc.cacheHits, 0)
	atomic.StoreUint64(&sc.cacheMisses, 0)
	atomic.StoreUint64(&sc.failoverCount, 0)
}

// statsMiddleware collects statistics about operations
type statsMiddleware struct {
	collector *StatsCollector
	startTime time.Time
}

func (sm *statsMiddleware) Before(ctx *OperationContext) error {
	sm.startTime = time.Now()
	return nil
}

func (sm *statsMiddleware) After(ctx *OperationContext) {
	duration := time.Since(sm.startTime)

	pattern := ""
	if ctx.Route != nil {
		pattern = ctx.Route.Pattern
	}

	sm.collector.RecordOperation(pattern, ctx.Operation, duration, ctx.Error)

	if pattern != "" {
		sm.collector.RecordRouteHit(pattern)
	}
}

// NewStatsMiddleware creates a middleware that collects statistics
func NewStatsMiddleware(collector *StatsCollector) Middleware {
	return &statsMiddleware{collector: collector}
}
