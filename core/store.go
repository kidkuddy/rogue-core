package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type sqliteStore struct {
	dataDir string
	logger  *slog.Logger
}

func NewSQLiteStore(dataDir string, logger *slog.Logger) Store {
	return &sqliteStore{
		dataDir: dataDir,
		logger:  logger,
	}
}

func (s *sqliteStore) Namespace(name string) Namespace {
	return &sqliteNamespace{
		name:    name,
		dbDir:   filepath.Join(s.dataDir, "db", name),
		fileDir: filepath.Join(s.dataDir, "files", name),
		logger:  s.logger,
	}
}

func (s *sqliteStore) Backup(ctx context.Context) error {
	s.logger.Info("backup requested")
	// TODO: WAL checkpoint all databases, git commit
	return nil
}

func (s *sqliteStore) Close() error {
	s.logger.Info("store closing")
	// TODO: Close all open database connections
	return nil
}

type sqliteNamespace struct {
	name    string
	dbDir   string
	fileDir string
	logger  *slog.Logger
}

func (n *sqliteNamespace) DB() (*sql.DB, error) {
	if err := os.MkdirAll(n.dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db dir for namespace %s: %w", n.name, err)
	}

	dbPath := filepath.Join(n.dbDir, "store.sqlite")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database for namespace %s: %w", n.name, err)
	}

	n.logger.Info("database opened", "namespace", n.name, "path", dbPath)
	return db, nil
}

func (n *sqliteNamespace) FilePath(name string) string {
	return filepath.Join(n.fileDir, name)
}

func (n *sqliteNamespace) FileList() ([]FileInfo, error) {
	if err := os.MkdirAll(n.fileDir, 0755); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(n.fileDir)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return files, nil
}

func (n *sqliteNamespace) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(n.fileDir, name))
}

func (n *sqliteNamespace) WriteFile(name string, data []byte) error {
	if err := os.MkdirAll(n.fileDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(n.fileDir, name), data, 0644)
}

// stubStore is used in tests when no real storage is needed.
type stubStore struct{}

func NewStubStore() Store { return &stubStore{} }

func (s *stubStore) Namespace(name string) Namespace {
	return &stubNamespace{name: name}
}
func (s *stubStore) Backup(ctx context.Context) error { return nil }
func (s *stubStore) Close() error                     { return nil }

type stubNamespace struct{ name string }

func (n *stubNamespace) DB() (*sql.DB, error)                { return nil, nil }
func (n *stubNamespace) FilePath(name string) string         { return "/dev/null" }
func (n *stubNamespace) FileList() ([]FileInfo, error)       { return nil, nil }
func (n *stubNamespace) ReadFile(name string) ([]byte, error) {
	return nil, fmt.Errorf("stub: file not found")
}
func (n *stubNamespace) WriteFile(name string, data []byte) error { return nil }

// ensure interfaces are satisfied
var _ Store = (*sqliteStore)(nil)
var _ Store = (*stubStore)(nil)
var _ Namespace = (*sqliteNamespace)(nil)
var _ Namespace = (*stubNamespace)(nil)
var _ time.Time // prevent unused import warning
