// Package metrics provides a lightweight Prometheus-compatible metrics registry
// for IMAgent Relay. No external dependencies — pure text format output.
package metrics

import (
	"fmt"
	"math"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds all metrics.
type Registry struct {
	gauges   map[string]*Gauge
	counters map[string]*Counter
	mu       sync.RWMutex
	started  time.Time
}

// Gauge is a value that goes up and down.
type Gauge struct {
	value int64 // atomic
	help  string
}

// Counter is a monotonically increasing value.
type Counter struct {
	value int64 // atomic
	help  string
}

// NewRegistry creates a metrics registry.
func NewRegistry() *Registry {
	r := &Registry{
		gauges:   make(map[string]*Gauge),
		counters: make(map[string]*Counter),
		started:  time.Now(),
	}
	// Built-in Go metrics
	r.Gauge("go_goroutines", "Number of goroutines")
	r.Gauge("go_mem_alloc_bytes", "Bytes of allocated heap objects")
	return r
}

// Gauge creates or returns a gauge.
func (r *Registry) Gauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{help: help}
	r.gauges[name] = g
	return g
}

// Set sets the gauge value.
func (g *Gauge) Set(v int64) {
	atomic.StoreInt64(&g.value, v)
}

// Inc increments the gauge.
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

// Dec decrements the gauge.
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return atomic.LoadInt64(&g.value)
}

// Counter creates or returns a counter.
func (r *Registry) Counter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{help: help}
	r.counters[name] = c
	return c
}

// Inc increments the counter.
func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add adds delta to the counter.
func (c *Counter) Add(delta int64) {
	atomic.AddInt64(&c.value, delta)
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	return atomic.LoadInt64(&c.value)
}

// HandleMetrics returns an HTTP handler that serves Prometheus text format.
func (r *Registry) HandleMetrics(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Update built-ins
	if g, ok := r.gauges["go_goroutines"]; ok {
		g.Set(int64(runtime.NumGoroutine()))
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if g, ok := r.gauges["go_mem_alloc_bytes"]; ok {
		g.Set(int64(m.Alloc))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	var sb strings.Builder

	// Gauges
	for name, g := range r.gauges {
		if g.help != "" {
			sb.WriteString(fmt.Sprintf("# HELP %s %s\n", name, g.help))
		}
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
		sb.WriteString(fmt.Sprintf("%s %d\n", name, g.Value()))
	}

	// Counters
	for name, c := range r.counters {
		if c.help != "" {
			sb.WriteString(fmt.Sprintf("# HELP %s %s\n", name, c.help))
		}
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n", name))
		sb.WriteString(fmt.Sprintf("%s %d\n", name, c.Value()))
	}

	// Uptime
	uptime := time.Since(r.started).Seconds()
	sb.WriteString(fmt.Sprintf("# HELP relay_uptime_seconds Relay process uptime\n"))
	sb.WriteString(fmt.Sprintf("# TYPE relay_uptime_seconds gauge\n"))
	sb.WriteString(fmt.Sprintf("relay_uptime_seconds %d\n", int64(uptime)))

	w.Write([]byte(sb.String()))
}

// Snapshot returns a map of all metric names to values.
func (r *Registry) Snapshot() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := make(map[string]int64)
	for name, g := range r.gauges {
		snap[name] = g.Value()
	}
	for name, c := range r.counters {
		snap[name] = c.Value()
	}
	snap["relay_uptime_seconds"] = int64(time.Since(r.started).Seconds())
	return snap
}

// ---------- Adaptive utilities ----------

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	attempts   int32
}

// NewBackoff creates a backoff helper.
func NewBackoff() *Backoff {
	return &Backoff{
		Initial:    1 * time.Second,
		Max:        5 * time.Minute,
		Multiplier: 2.0,
	}
}

// Next returns the next backoff duration and increments attempts.
func (b *Backoff) Next() time.Duration {
	n := atomic.AddInt32(&b.attempts, 1) - 1
	d := float64(b.Initial) * math.Pow(b.Multiplier, float64(n))
	if d > float64(b.Max) {
		d = float64(b.Max)
	}
	return time.Duration(d)
}

// Reset clears the attempt counter.
func (b *Backoff) Reset() {
	atomic.StoreInt32(&b.attempts, 0)
}

// Attempts returns the current attempt count.
func (b *Backoff) Attempts() int {
	return int(atomic.LoadInt32(&b.attempts))
}

// CircuitBreaker implements a simple circuit breaker pattern.
type CircuitBreaker struct {
	failures    int32
	threshold   int32
	resetAfter  time.Duration
	lastFailure time.Time
	open        int32 // atomic bool: 0=closed, 1=open
	mu          sync.Mutex
}

// NewCircuitBreaker creates a circuit breaker.
// threshold: consecutive failures before opening
// resetAfter: how long to wait before trying again
func NewCircuitBreaker(threshold int32, resetAfter time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:  threshold,
		resetAfter: resetAfter,
	}
}

// Allow returns true if the circuit is closed (requests allowed).
func (cb *CircuitBreaker) Allow() bool {
	if atomic.LoadInt32(&cb.open) == 0 {
		return true
	}
	// Check if reset time has elapsed
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if time.Since(cb.lastFailure) > cb.resetAfter {
		atomic.StoreInt32(&cb.open, 0)
		atomic.StoreInt32(&cb.failures, 0)
		return true
	}
	return false
}

// RecordFailure records a failure and may open the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	f := atomic.AddInt32(&cb.failures, 1)
	cb.mu.Lock()
	cb.lastFailure = time.Now()
	cb.mu.Unlock()
	if f >= cb.threshold {
		atomic.StoreInt32(&cb.open, 1)
	}
}

// RecordSuccess resets the failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	atomic.StoreInt32(&cb.failures, 0)
	atomic.StoreInt32(&cb.open, 0)
}

// IsOpen returns true if the circuit is open.
func (cb *CircuitBreaker) IsOpen() bool {
	return atomic.LoadInt32(&cb.open) == 1
}

// ResetAfter returns the cooldown duration.
func (cb *CircuitBreaker) ResetAfter() time.Duration {
	return cb.resetAfter
}
