package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

const sqliteDriverName = "sqlite"

var errUsageRepositoryClosed = errors.New("usage repository closed")

// SQLiteRepositoryConfig configures the SQLite-backed usage event store.
type SQLiteRepositoryConfig struct {
	Path              string
	BatchSize         int
	FlushInterval     time.Duration
	RetentionDays     int
	RetentionInterval time.Duration
	QueueSize         int
	BusyTimeout       time.Duration
}

// SQLiteRepository persists usage events asynchronously and reconstructs legacy snapshots on demand.
type SQLiteRepository struct {
	db                *sql.DB
	path              string
	batchSize         int
	flushInterval     time.Duration
	retentionDays     int
	retentionInterval time.Duration
	events            chan UsageEvent
	flushRequests     chan chan error
	workerWG          sync.WaitGroup
	closeOnce         sync.Once
	enqueueMu         sync.RWMutex
	closed            bool
}

// NewSQLiteRepository opens the SQLite database, ensures schema, and starts the async flush worker.
func NewSQLiteRepository(ctx context.Context, cfg SQLiteRepositoryConfig) (*SQLiteRepository, error) {
	path := cfg.Path
	if path == "" {
		return nil, fmt.Errorf("usage sqlite repository: path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("usage sqlite repository: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("usage sqlite repository: create directory: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	db, err := sql.Open(sqliteDriverName, absPath)
	if err != nil {
		return nil, fmt.Errorf("usage sqlite repository: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	repo := &SQLiteRepository{
		db:                db,
		path:              absPath,
		batchSize:         normaliseBatchSize(cfg.BatchSize),
		flushInterval:     normaliseFlushInterval(cfg.FlushInterval),
		retentionDays:     normaliseRetentionDays(cfg.RetentionDays),
		retentionInterval: normaliseRetentionInterval(cfg.RetentionInterval),
		events:            make(chan UsageEvent, normaliseQueueSize(cfg.QueueSize, cfg.BatchSize)),
		flushRequests:     make(chan chan error),
	}

	if err := repo.configure(ctx, normaliseBusyTimeout(cfg.BusyTimeout)); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.ensureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.pruneExpired(ctx, time.Now().UTC()); err != nil {
		_ = db.Close()
		return nil, err
	}

	repo.workerWG.Add(1)
	go repo.run()
	return repo, nil
}

func (r *SQLiteRepository) configure(ctx context.Context, busyTimeout time.Duration) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout.Milliseconds()),
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = MEMORY",
	}
	for _, pragma := range pragmas {
		if _, err := r.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("usage sqlite repository: configure pragma %q: %w", pragma, err)
		}
	}
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("usage sqlite repository: ping database: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) ensureSchema(ctx context.Context) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("usage sqlite repository: not initialized")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS usage_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requested_at_ns INTEGER NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			api_key TEXT NOT NULL,
			request_method TEXT NOT NULL DEFAULT '',
			request_path TEXT NOT NULL DEFAULT '',
			auth_id TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			source TEXT NOT NULL,
			latency_ms INTEGER NOT NULL,
			first_token_latency_ms INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			dedup_key TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_requested_at_ns ON usage_events(requested_at_ns)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_requested_at_ns_id ON usage_events(requested_at_ns DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_api_key_requested_at_ns ON usage_events(api_key, requested_at_ns)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_model_requested_at_ns ON usage_events(model, requested_at_ns)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_dedup_key ON usage_events(dedup_key)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("usage sqlite repository: ensure schema: %w", err)
		}
	}
	alterStatements := []string{
		`ALTER TABLE usage_events ADD COLUMN request_method TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE usage_events ADD COLUMN request_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE usage_events ADD COLUMN first_token_latency_ms INTEGER NOT NULL DEFAULT 0`,
	}
	for _, statement := range alterStatements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return fmt.Errorf("usage sqlite repository: ensure schema alter: %w", err)
		}
	}
	return nil
}

// Path returns the resolved absolute SQLite file path.
func (r *SQLiteRepository) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Record queues a usage event for batched persistence.
func (r *SQLiteRepository) Record(ctx context.Context, event UsageEvent) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	event = normaliseUsageEvent(event)

	r.enqueueMu.RLock()
	if r.closed {
		r.enqueueMu.RUnlock()
		return errUsageRepositoryClosed
	}
	defer r.enqueueMu.RUnlock()

	select {
	case r.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Snapshot flushes pending writes and reconstructs the compatibility snapshot from SQLite.
func (r *SQLiteRepository) Snapshot(ctx context.Context) (StatisticsSnapshot, error) {
	if r == nil {
		return StatisticsSnapshot{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.flush(ctx); err != nil {
		return StatisticsSnapshot{}, err
	}
	return r.snapshotFromDatabase(ctx)
}

// ImportSnapshot merges a legacy snapshot into SQLite using the historical dedup semantics.
func (r *SQLiteRepository) ImportSnapshot(ctx context.Context, snapshot StatisticsSnapshot) (MergeResult, error) {
	result := MergeResult{}
	if r == nil {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.flush(ctx); err != nil {
		return result, err
	}

	cutoff := retentionCutoff(time.Now().UTC(), r.retentionDays)
	pending := make([]UsageEvent, 0)
	seen := make(map[string]struct{})
	for apiName, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				event, ok := ImportedUsageEvent(apiName, modelName, detail)
				if !ok {
					continue
				}
				if event.RequestedAt.Before(cutoff) {
					result.Skipped++
					continue
				}
				if _, exists := seen[event.DedupKey]; exists {
					result.Skipped++
					continue
				}
				exists, err := r.hasDedupKey(ctx, event.DedupKey)
				if err != nil {
					return result, err
				}
				if exists {
					result.Skipped++
					seen[event.DedupKey] = struct{}{}
					continue
				}
				seen[event.DedupKey] = struct{}{}
				pending = append(pending, event)
			}
		}
	}

	if len(pending) == 0 {
		return result, nil
	}
	if err := r.insertBatch(ctx, pending); err != nil {
		return result, err
	}
	result.Added = int64(len(pending))
	if err := r.pruneExpired(ctx, time.Now().UTC()); err != nil {
		return result, err
	}
	return result, nil
}

// Close flushes pending events and closes the SQLite database.
func (r *SQLiteRepository) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.closeOnce.Do(func() {
		r.enqueueMu.Lock()
		r.closed = true
		close(r.events)
		r.enqueueMu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.workerWG.Wait()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *SQLiteRepository) flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.enqueueMu.Lock()
	if r.closed {
		r.enqueueMu.Unlock()
		return errUsageRepositoryClosed
	}
	defer r.enqueueMu.Unlock()

	response := make(chan error, 1)
	select {
	case r.flushRequests <- response:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *SQLiteRepository) drainQueuedEvents(batch *[]UsageEvent) bool {
	if r == nil {
		return false
	}
	for {
		select {
		case event, ok := <-r.events:
			if !ok {
				return true
			}
			*batch = append(*batch, event)
		default:
			return false
		}
	}
}

func (r *SQLiteRepository) run() {
	defer r.workerWG.Done()

	flushTicker := time.NewTicker(r.flushInterval)
	retentionTicker := time.NewTicker(r.retentionInterval)
	defer flushTicker.Stop()
	defer retentionTicker.Stop()

	batch := make([]UsageEvent, 0, r.batchSize)
	flushBatch := func(ctx context.Context) error {
		if len(batch) == 0 {
			return nil
		}
		pending := append(make([]UsageEvent, 0, len(batch)), batch...)
		batch = batch[:0]
		return r.insertBatch(ctx, pending)
	}

	for {
		select {
		case event, ok := <-r.events:
			if !ok {
				if err := flushBatch(context.Background()); err != nil {
					log.WithError(err).Warn("usage: final SQLite flush failed during shutdown")
				}
				return
			}
			batch = append(batch, event)
			if len(batch) >= r.batchSize {
				if err := flushBatch(context.Background()); err != nil {
					log.WithError(err).Warn("usage: batch SQLite flush failed")
				}
			}
		case response := <-r.flushRequests:
			if r.drainQueuedEvents(&batch) {
				response <- flushBatch(context.Background())
				return
			}
			response <- flushBatch(context.Background())
		case <-flushTicker.C:
			if err := flushBatch(context.Background()); err != nil {
				log.WithError(err).Warn("usage: periodic SQLite flush failed")
			}
		case <-retentionTicker.C:
			if err := flushBatch(context.Background()); err != nil {
				log.WithError(err).Warn("usage: retention pre-flush failed")
			}
			if err := r.pruneExpired(context.Background(), time.Now().UTC()); err != nil {
				log.WithError(err).Warn("usage: retention prune failed")
			}
		}
	}
}

func (r *SQLiteRepository) insertBatch(ctx context.Context, batch []UsageEvent) (err error) {
	if r == nil || len(batch) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("usage sqlite repository: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO usage_events (
		requested_at_ns,
		provider,
		model,
		api_key,
		request_method,
		request_path,
		auth_id,
		auth_index,
		source,
		latency_ms,
		first_token_latency_ms,
		failed,
		input_tokens,
		output_tokens,
		reasoning_tokens,
		cached_tokens,
		total_tokens,
		dedup_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("usage sqlite repository: prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, event := range batch {
		event = normaliseUsageEvent(event)
		if _, err = stmt.ExecContext(
			ctx,
			event.RequestedAt.UTC().UnixNano(),
			event.Provider,
			event.Model,
			event.APIKey,
			event.RequestMethod,
			event.RequestPath,
			event.AuthID,
			event.AuthIndex,
			event.Source,
			event.LatencyMs,
			event.FirstTokenLatencyMs,
			boolToInt(event.Failed),
			event.Tokens.InputTokens,
			event.Tokens.OutputTokens,
			event.Tokens.ReasoningTokens,
			event.Tokens.CachedTokens,
			event.Tokens.TotalTokens,
			event.DedupKey,
		); err != nil {
			return fmt.Errorf("usage sqlite repository: insert event: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("usage sqlite repository: commit transaction: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) pruneExpired(ctx context.Context, now time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cutoff := retentionCutoff(now, r.retentionDays).UnixNano()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM usage_events WHERE requested_at_ns < ?`, cutoff); err != nil {
		return fmt.Errorf("usage sqlite repository: prune expired events: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) hasDedupKey(ctx context.Context, dedupKey string) (bool, error) {
	if r == nil || r.db == nil {
		return false, nil
	}
	row := r.db.QueryRowContext(ctx, `SELECT 1 FROM usage_events WHERE dedup_key = ? LIMIT 1`, dedupKey)
	var marker int
	if err := row.Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("usage sqlite repository: check dedup key: %w", err)
	}
	return true, nil
}

func (r *SQLiteRepository) snapshotFromDatabase(ctx context.Context) (StatisticsSnapshot, error) {
	if r == nil || r.db == nil {
		return StatisticsSnapshot{}, nil
	}
	cutoff := retentionCutoff(time.Now().UTC(), r.retentionDays).UnixNano()
	rows, err := r.db.QueryContext(ctx, `SELECT
		requested_at_ns,
		provider,
		model,
		api_key,
		auth_id,
		auth_index,
		source,
		latency_ms,
		first_token_latency_ms,
		failed,
		input_tokens,
		output_tokens,
		reasoning_tokens,
		cached_tokens,
		total_tokens
	FROM usage_events
	WHERE requested_at_ns >= ?
	ORDER BY requested_at_ns ASC, id ASC`, cutoff)
	if err != nil {
		return StatisticsSnapshot{}, fmt.Errorf("usage sqlite repository: query snapshot: %w", err)
	}
	defer rows.Close()

	stats := NewRequestStatistics()
	for rows.Next() {
		var (
			requestedAtNS       int64
			provider            string
			model               string
			apiKey              string
			authID              string
			authIndex           string
			source              string
			latencyMs           int64
			firstTokenLatencyMs int64
			failedInt           int
			inputTokens         int64
			outputTokens        int64
			reasoningTokens     int64
			cachedTokens        int64
			totalTokens         int64
		)
		if err := rows.Scan(
			&requestedAtNS,
			&provider,
			&model,
			&apiKey,
			&authID,
			&authIndex,
			&source,
			&latencyMs,
			&firstTokenLatencyMs,
			&failedInt,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cachedTokens,
			&totalTokens,
		); err != nil {
			return StatisticsSnapshot{}, fmt.Errorf("usage sqlite repository: scan snapshot row: %w", err)
		}

		event := normaliseUsageEvent(UsageEvent{
			RequestedAt:         time.Unix(0, requestedAtNS).UTC(),
			Provider:            provider,
			Model:               model,
			APIKey:              apiKey,
			AuthID:              authID,
			AuthIndex:           authIndex,
			Source:              source,
			LatencyMs:           latencyMs,
			FirstTokenLatencyMs: firstTokenLatencyMs,
			Failed:              failedInt != 0,
			Tokens: TokenStats{
				InputTokens:     inputTokens,
				OutputTokens:    outputTokens,
				ReasoningTokens: reasoningTokens,
				CachedTokens:    cachedTokens,
				TotalTokens:     totalTokens,
			},
		})
		stats.recordUsageEvent(event)
	}
	if err := rows.Err(); err != nil {
		return StatisticsSnapshot{}, fmt.Errorf("usage sqlite repository: iterate snapshot rows: %w", err)
	}
	return stats.Snapshot(), nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
