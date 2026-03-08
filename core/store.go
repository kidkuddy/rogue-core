package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	dataDir string
	dbs     map[string]*sql.DB
	mu      sync.RWMutex
	logger  *slog.Logger
}

func NewSQLiteStore(dataDir string, logger *slog.Logger) Store {
	return &sqliteStore{
		dataDir: dataDir,
		dbs:     make(map[string]*sql.DB),
		logger:  logger,
	}
}

func (s *sqliteStore) Namespace(name string) Namespace {
	return &sqliteNamespace{
		store:   s,
		name:    name,
		dbDir:   filepath.Join(s.dataDir, name, "db"),
		fileDir: filepath.Join(s.dataDir, name, "files"),
		logger:  s.logger,
	}
}

func (s *sqliteStore) Backup(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, db := range s.dbs {
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			s.logger.Error("wal checkpoint failed", "namespace", name, "error", err)
		} else {
			s.logger.Info("wal checkpoint complete", "namespace", name)
		}
	}
	return nil
}

func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for name, db := range s.dbs {
		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))
		}
	}
	s.dbs = make(map[string]*sql.DB)
	s.logger.Info("store closed", "databases", len(s.dbs))

	if len(errs) > 0 {
		return fmt.Errorf("errors closing databases: %v", errs)
	}
	return nil
}

// getOrOpenDB returns a cached DB handle or opens a new one.
func (s *sqliteStore) getOrOpenDB(name, dbDir string) (*sql.DB, error) {
	s.mu.RLock()
	if db, ok := s.dbs[name]; ok {
		s.mu.RUnlock()
		return db, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if db, ok := s.dbs[name]; ok {
		return db, nil
	}

	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir for %s: %w", name, err)
	}

	dbPath := filepath.Join(dbDir, "store.sqlite")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", name, err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database %s: %w", name, err)
	}

	s.dbs[name] = db
	s.logger.Info("database opened", "namespace", name, "path", dbPath)
	return db, nil
}

type sqliteNamespace struct {
	store   *sqliteStore
	name    string
	dbDir   string
	fileDir string
	logger  *slog.Logger
}

func (n *sqliteNamespace) DB() (*sql.DB, error) {
	return n.store.getOrOpenDB(n.name, n.dbDir)
}

func (n *sqliteNamespace) FilePath(name string) string {
	cleaned := n.safePath(name)
	return filepath.Join(n.fileDir, cleaned)
}

func (n *sqliteNamespace) FileList() ([]FileInfo, error) {
	if err := os.MkdirAll(n.fileDir, 0755); err != nil {
		return nil, err
	}
	return listFilesRecursive(n.fileDir, n.fileDir)
}

func (n *sqliteNamespace) ReadFile(name string) ([]byte, error) {
	cleaned := n.safePath(name)
	path := filepath.Join(n.fileDir, cleaned)

	// Ensure the resolved path stays within namespace
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %s", name)
	}
	absFileDir, _ := filepath.Abs(n.fileDir)
	if !strings.HasPrefix(absPath, absFileDir) {
		return nil, fmt.Errorf("path traversal denied: %s", name)
	}

	return os.ReadFile(path)
}

func (n *sqliteNamespace) WriteFile(name string, data []byte) error {
	cleaned := n.safePath(name)
	path := filepath.Join(n.fileDir, cleaned)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// safePath cleans a path and strips leading slashes / ".." traversal.
func (n *sqliteNamespace) safePath(name string) string {
	cleaned := filepath.Clean(name)
	// Remove leading slashes
	cleaned = strings.TrimLeft(cleaned, "/")
	// Remove any ".." components
	parts := strings.Split(cleaned, string(filepath.Separator))
	var safe []string
	for _, p := range parts {
		if p != ".." && p != "." {
			safe = append(safe, p)
		}
	}
	if len(safe) == 0 {
		return "unnamed"
	}
	return filepath.Join(safe...)
}

func listFilesRecursive(root, dir string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			sub, err := listFilesRecursive(root, fullPath)
			if err != nil {
				continue
			}
			files = append(files, sub...)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(root, fullPath)
		files = append(files, FileInfo{
			Name:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return files, nil
}

// --- Stub implementations for testing ---

type stubStore struct{}

func NewStubStore() Store { return &stubStore{} }

func (s *stubStore) Namespace(name string) Namespace {
	return &stubNamespace{name: name}
}
func (s *stubStore) Backup(ctx context.Context) error { return nil }
func (s *stubStore) Close() error                     { return nil }

type stubNamespace struct{ name string }

func (n *stubNamespace) DB() (*sql.DB, error)                     { return nil, nil }
func (n *stubNamespace) FilePath(name string) string              { return "/dev/null" }
func (n *stubNamespace) FileList() ([]FileInfo, error)            { return nil, nil }
func (n *stubNamespace) ReadFile(name string) ([]byte, error)     { return nil, fmt.Errorf("stub: file not found") }
func (n *stubNamespace) WriteFile(name string, data []byte) error { return nil }

var _ Store = (*sqliteStore)(nil)
var _ Store = (*stubStore)(nil)
var _ Namespace = (*sqliteNamespace)(nil)
var _ Namespace = (*stubNamespace)(nil)
