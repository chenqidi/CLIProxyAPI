package usage

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	Provider          string
	Model             string
	APIKey            string
	AuthID            string
	AuthIndex         string
	Source            string
	RequestedAt       time.Time
	Latency           time.Duration
	FirstTokenLatency time.Duration
	Failed            bool
	Detail            Detail
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
}

type streamTimingContextKey struct{}

type streamTimings struct {
	startedAt          time.Time
	firstPayloadUnixNs atomic.Int64
}

// WithStreamTimings returns a context that tracks downstream-visible streaming timings.
func WithStreamTimings(ctx context.Context, startedAt time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return context.WithValue(ctx, streamTimingContextKey{}, &streamTimings{startedAt: startedAt})
}

// MarkFirstStreamPayload records the first downstream-visible streaming payload time.
func MarkFirstStreamPayload(ctx context.Context) {
	timings := streamTimingsFromContext(ctx)
	if timings == nil || timings.startedAt.IsZero() {
		return
	}
	timings.firstPayloadUnixNs.CompareAndSwap(0, time.Now().UnixNano())
}

// FirstTokenLatencyFromContext returns the recorded first streaming payload latency.
func FirstTokenLatencyFromContext(ctx context.Context) time.Duration {
	timings := streamTimingsFromContext(ctx)
	if timings == nil || timings.startedAt.IsZero() {
		return 0
	}
	firstPayloadUnixNs := timings.firstPayloadUnixNs.Load()
	if firstPayloadUnixNs <= 0 {
		return 0
	}
	latency := time.Unix(0, firstPayloadUnixNs).Sub(timings.startedAt)
	if latency < 0 {
		return 0
	}
	return latency
}

func streamTimingsFromContext(ctx context.Context) *streamTimings {
	if ctx == nil {
		return nil
	}
	timings, ok := ctx.Value(streamTimingContextKey{}).(*streamTimings)
	if !ok {
		return nil
	}
	return timings
}

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

type queueItem struct {
	ctx    context.Context
	record Record
}

// Manager maintains a queue of usage records and delivers them to registered plugins.
type Manager struct {
	once     sync.Once
	stopOnce sync.Once
	cancel   context.CancelFunc

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []queueItem
	closed bool

	pluginsMu sync.RWMutex
	plugins   []Plugin
}

// NewManager constructs a manager with a buffered queue.
func NewManager(buffer int) *Manager {
	m := &Manager{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// Start launches the background dispatcher. Calling Start multiple times is safe.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		var workerCtx context.Context
		workerCtx, m.cancel = context.WithCancel(ctx)
		go m.run(workerCtx)
	})
}

// Stop stops the dispatcher and drains the queue.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.cond.Broadcast()
	})
}

// Register appends a plugin to the delivery list.
func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.pluginsMu.Lock()
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}

// Publish enqueues a usage record for processing. If no plugin is registered
// the record will be discarded downstream.
func (m *Manager) Publish(ctx context.Context, record Record) {
	if m == nil {
		return
	}
	// ensure worker is running even if Start was not called explicitly
	m.Start(context.Background())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.queue = append(m.queue, queueItem{ctx: ctx, record: record})
	m.mu.Unlock()
	m.cond.Signal()
}

func (m *Manager) run(ctx context.Context) {
	for {
		m.mu.Lock()
		for !m.closed && len(m.queue) == 0 {
			m.cond.Wait()
		}
		if len(m.queue) == 0 && m.closed {
			m.mu.Unlock()
			return
		}
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.mu.Unlock()
		m.dispatch(item)
	}
}

func (m *Manager) dispatch(item queueItem) {
	m.pluginsMu.RLock()
	plugins := make([]Plugin, len(m.plugins))
	copy(plugins, m.plugins)
	m.pluginsMu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		safeInvoke(plugin, item.ctx, item.record)
	}
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("usage: plugin panic recovered: %v", r)
		}
	}()
	plugin.HandleUsage(ctx, record)
}

var defaultManager = NewManager(512)

// DefaultManager returns the global usage manager instance.
func DefaultManager() *Manager { return defaultManager }

// RegisterPlugin registers a plugin on the default manager.
func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }

// PublishRecord publishes a record using the default manager.
func PublishRecord(ctx context.Context, record Record) { DefaultManager().Publish(ctx, record) }

// StartDefault starts the default manager's dispatcher.
func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }

// StopDefault stops the default manager's dispatcher.
func StopDefault() { DefaultManager().Stop() }
