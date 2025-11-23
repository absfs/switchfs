package switchfs

import (
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// BackendHealth tracks the health status of a backend
type BackendHealth struct {
	Healthy       bool
	FailureCount  int
	LastFailure   time.Time
	LastSuccess   time.Time
	CircuitOpen   bool
	CircuitOpened time.Time
}

// HealthMonitor monitors backend health and manages circuit breakers
type HealthMonitor struct {
	mu               sync.RWMutex
	backends         map[absfs.FileSystem]*BackendHealth
	failureThreshold int           // Number of failures before opening circuit
	circuitTimeout   time.Duration // How long circuit stays open
	recoveryTimeout  time.Duration // Time to wait before trying recovery
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(failureThreshold int, circuitTimeout, recoveryTimeout time.Duration) *HealthMonitor {
	return &HealthMonitor{
		backends:         make(map[absfs.FileSystem]*BackendHealth),
		failureThreshold: failureThreshold,
		circuitTimeout:   circuitTimeout,
		recoveryTimeout:  recoveryTimeout,
	}
}

// RecordSuccess records a successful operation for a backend
func (hm *HealthMonitor) RecordSuccess(backend absfs.FileSystem) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	health, ok := hm.backends[backend]
	if !ok {
		health = &BackendHealth{Healthy: true}
		hm.backends[backend] = health
	}

	health.LastSuccess = time.Now()
	health.FailureCount = 0
	health.Healthy = true

	// Close circuit if it was open
	if health.CircuitOpen {
		health.CircuitOpen = false
	}
}

// RecordFailure records a failed operation for a backend
func (hm *HealthMonitor) RecordFailure(backend absfs.FileSystem) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	health, ok := hm.backends[backend]
	if !ok {
		health = &BackendHealth{Healthy: true}
		hm.backends[backend] = health
	}

	health.LastFailure = time.Now()
	health.FailureCount++

	// Open circuit if failure threshold exceeded
	if health.FailureCount >= hm.failureThreshold {
		health.CircuitOpen = true
		health.CircuitOpened = time.Now()
		health.Healthy = false
	}
}

// IsHealthy checks if a backend is healthy
func (hm *HealthMonitor) IsHealthy(backend absfs.FileSystem) bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	health, ok := hm.backends[backend]
	if !ok {
		return true // Unknown backends are assumed healthy
	}

	// Check if circuit is open
	if health.CircuitOpen {
		// Check if enough time has passed to try recovery
		if time.Since(health.CircuitOpened) > hm.circuitTimeout {
			// Allow one retry to test recovery
			return true
		}
		return false
	}

	return health.Healthy
}

// GetHealth returns the health status of a backend
func (hm *HealthMonitor) GetHealth(backend absfs.FileSystem) *BackendHealth {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	health, ok := hm.backends[backend]
	if !ok {
		return &BackendHealth{Healthy: true}
	}

	// Create a copy to avoid race conditions
	return &BackendHealth{
		Healthy:       health.Healthy,
		FailureCount:  health.FailureCount,
		LastFailure:   health.LastFailure,
		LastSuccess:   health.LastSuccess,
		CircuitOpen:   health.CircuitOpen,
		CircuitOpened: health.CircuitOpened,
	}
}

// Reset resets the health status of a backend
func (hm *HealthMonitor) Reset(backend absfs.FileSystem) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	delete(hm.backends, backend)
}

// ResetAll resets all health statuses
func (hm *HealthMonitor) ResetAll() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.backends = make(map[absfs.FileSystem]*BackendHealth)
}
