package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage/lock"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db      *sql.DB
	logger  *logging.Logger
	dbPath  string
	lock    *lock.FileLock
	cfgMeta config.Metadata
}

func New(cfg config.Config, meta config.Metadata, logger *logging.Logger) (*Store, error) {
	dataDir := config.ResolveDataDir(meta)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStoragePermission, "failed to create data dir", err)
	}

	dbPath := strings.TrimSpace(cfg.Storage.Path)
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "quiverkeep.db")
	}

	lockPath := filepath.Join(dataDir, "quiverkeep.lock")
	fileLock, err := lock.Acquire(lockPath, logger)
	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Info("opening sqlite database", "db_path", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		_ = fileLock.Release()
		return nil, qerrors.Wrap(qerrors.CodeStorageOpen, "failed opening sqlite", err)
	}

	store := &Store{
		db:      db,
		logger:  logger,
		dbPath:  dbPath,
		lock:    fileLock,
		cfgMeta: meta,
	}

	if err := store.configureConnection(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.runMigrations(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := ensureFilePermission(dbPath); err != nil {
		_ = store.Close()
		return nil, qerrors.Wrap(qerrors.CodeStoragePermission, "failed setting sqlite file permissions", err)
	}

	return store, nil
}

func (s *Store) configureConnection() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return qerrors.Wrap(qerrors.CodeStorageOpen, "failed setting sqlite journal mode", err)
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		return qerrors.Wrap(qerrors.CodeStorageOpen, "failed enabling sqlite foreign keys", err)
	}
	if _, err := s.db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		return qerrors.Wrap(qerrors.CodeStorageOpen, "failed setting sqlite busy timeout", err)
	}
	return nil
}

func (s *Store) runMigrations(ctx context.Context) error {
	script, err := migrationFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		return qerrors.Wrap(qerrors.CodeStorageMigration, "failed reading migration file", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return qerrors.Wrap(qerrors.CodeStorageMigration, "failed starting migration tx", err)
	}

	if s.logger != nil {
		s.logger.Info("migration start", "version", 1)
	}

	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		_ = tx.Rollback()
		if s.logger != nil {
			s.logger.Error("migration failed", "version", 1, "error", err)
		}
		return qerrors.Wrap(qerrors.CodeStorageMigration, "failed applying migration 001", err)
	}

	if err := tx.Commit(); err != nil {
		if s.logger != nil {
			s.logger.Error("migration commit failed", "version", 1, "error", err)
		}
		return qerrors.Wrap(qerrors.CodeStorageMigration, "failed committing migration", err)
	}

	if s.logger != nil {
		s.logger.Info("migration success", "version", 1)
	}

	return nil
}

func (s *Store) Close() error {
	var closeErr error
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			closeErr = err
		}
	}
	if s.lock != nil {
		if err := s.lock.Release(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return qerrors.Wrap(qerrors.CodeStorageOpen, "failed database ping", err)
	}
	return nil
}

func (s *Store) InsertUsage(ctx context.Context, usage domain.UsageRecord) error {
	const query = `INSERT INTO usage(service, model, tokens_in, tokens_out, cost, created_at) VALUES(?, ?, ?, ?, ?, ?);`
	createdAt := usage.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	if s.logger != nil {
		s.logger.Debug("insert usage", "service", usage.Service, "model", usage.Model, "tokens_in", usage.TokensIn, "tokens_out", usage.TokensOut, "cost", usage.Cost)
	}

	_, err := s.db.ExecContext(ctx, query, usage.Service, usage.Model, usage.TokensIn, usage.TokensOut, usage.Cost, createdAt.Format(time.RFC3339))
	if err != nil {
		return qerrors.Wrap(qerrors.CodeStorageQuery, "failed insert usage", err)
	}

	return nil
}

func (s *Store) ListUsage(ctx context.Context, filter domain.UsageFilter) ([]domain.UsageRecord, error) {
	query := `SELECT id, service, model, tokens_in, tokens_out, cost, created_at FROM usage`
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)

	if strings.TrimSpace(filter.Service) != "" {
		where = append(where, "service = ?")
		args = append(args, strings.TrimSpace(filter.Service))
	}
	if filter.From != nil {
		where = append(where, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(time.RFC3339))
	}
	if filter.To != nil {
		where = append(where, "created_at <= ?")
		args = append(args, filter.To.UTC().Format(time.RFC3339))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"

	started := time.Now()
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed query usage", err)
	}
	defer rows.Close()

	items := make([]domain.UsageRecord, 0)
	for rows.Next() {
		var row domain.UsageRecord
		var createdRaw string
		if err := rows.Scan(&row.ID, &row.Service, &row.Model, &row.TokensIn, &row.TokensOut, &row.Cost, &createdRaw); err != nil {
			return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed scan usage", err)
		}
		parsed, err := time.Parse(time.RFC3339, createdRaw)
		if err != nil {
			parsed = time.Now().UTC()
		}
		row.CreatedAt = parsed
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed iterate usage rows", err)
	}

	if s.logger != nil {
		duration := time.Since(started)
		if duration > 50*time.Millisecond {
			s.logger.Warn("slow usage query", "duration_ms", duration.Milliseconds(), "query", query)
		} else {
			s.logger.Debug("usage query completed", "duration_ms", duration.Milliseconds(), "items", len(items))
		}
	}

	return items, nil
}

func (s *Store) UsageSummary(ctx context.Context, filter domain.UsageFilter) (domain.UsageTotal, error) {
	items, err := s.ListUsage(ctx, filter)
	if err != nil {
		return domain.UsageTotal{}, err
	}
	var total domain.UsageTotal
	for _, item := range items {
		total.TokensIn += item.TokensIn
		total.TokensOut += item.TokensOut
		total.Cost += item.Cost
	}
	return total, nil
}

func (s *Store) UpsertSubscription(ctx context.Context, sub domain.Subscription) error {
	const query = `
INSERT INTO subscriptions(service, plan, limit_value, used, reset_date)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(service) DO UPDATE SET
	plan=excluded.plan,
	limit_value=excluded.limit_value,
	used=excluded.used,
	reset_date=excluded.reset_date;
`

	var resetValue any
	if sub.ResetDate != nil {
		resetValue = sub.ResetDate.UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx, query, sub.Service, sub.Plan, sub.LimitValue, sub.Used, resetValue)
	if err != nil {
		return qerrors.Wrap(qerrors.CodeStorageQuery, "failed upsert subscription", err)
	}
	return nil
}

func (s *Store) ListSubscriptions(ctx context.Context) ([]domain.Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT service, plan, limit_value, used, reset_date FROM subscriptions ORDER BY service ASC`)
	if err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed query subscriptions", err)
	}
	defer rows.Close()

	items := make([]domain.Subscription, 0)
	for rows.Next() {
		var item domain.Subscription
		var limit sql.NullInt64
		var reset sql.NullString
		if err := rows.Scan(&item.Service, &item.Plan, &limit, &item.Used, &reset); err != nil {
			return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed scan subscription", err)
		}
		if limit.Valid {
			value := limit.Int64
			item.LimitValue = &value
		}
		if reset.Valid {
			parsed, err := time.Parse(time.RFC3339, reset.String)
			if err == nil {
				item.ResetDate = &parsed
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, qerrors.Wrap(qerrors.CodeStorageQuery, "failed iterate subscriptions", err)
	}

	return items, nil
}

func (s *Store) Limits(ctx context.Context) ([]domain.LimitItem, error) {
	subs, err := s.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]domain.LimitItem, 0, len(subs))
	for _, sub := range subs {
		item := domain.LimitItem{
			Service:    sub.Service,
			Plan:       sub.Plan,
			LimitValue: sub.LimitValue,
			Used:       sub.Used,
			ResetDate:  sub.ResetDate,
			Percentage: nil,
		}

		if sub.LimitValue != nil && *sub.LimitValue > 0 {
			item.Percentage = float64(sub.Used) / float64(*sub.LimitValue) * 100
		}

		items = append(items, item)
	}
	return items, nil
}

func (s *Store) DbPath() string {
	return s.dbPath
}

func ensureFilePermission(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
	return nil
}
