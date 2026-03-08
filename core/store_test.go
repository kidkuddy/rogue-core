package core

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreNamespaceIsolation(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	// Two namespaces should get separate databases
	ns1 := store.Namespace("iam")
	ns2 := store.Namespace("power:memory")

	db1, err := ns1.DB()
	if err != nil {
		t.Fatalf("ns1 DB failed: %v", err)
	}
	db2, err := ns2.DB()
	if err != nil {
		t.Fatalf("ns2 DB failed: %v", err)
	}

	// Create a table in ns1
	if _, err := db1.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create table in ns1: %v", err)
	}
	if _, err := db1.Exec("INSERT INTO users (id, name) VALUES (1, 'alice')"); err != nil {
		t.Fatalf("insert into ns1: %v", err)
	}

	// Table should NOT exist in ns2
	_, err = db2.Exec("SELECT * FROM users")
	if err == nil {
		t.Fatal("expected error querying ns2 for ns1's table, got nil")
	}

	// Create different table in ns2
	if _, err := db2.Exec("CREATE TABLE papers (id INTEGER PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatalf("create table in ns2: %v", err)
	}

	// Verify separate db files exist
	if _, err := os.Stat(filepath.Join(dir, "iam", "db", "store.sqlite")); err != nil {
		t.Fatalf("ns1 db file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "power:memory", "db", "store.sqlite")); err != nil {
		t.Fatalf("ns2 db file missing: %v", err)
	}
}

func TestStoreDBCaching(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	ns := store.Namespace("iam")

	db1, err := ns.DB()
	if err != nil {
		t.Fatalf("first DB call: %v", err)
	}
	db2, err := ns.DB()
	if err != nil {
		t.Fatalf("second DB call: %v", err)
	}

	// Should return the same handle
	if db1 != db2 {
		t.Error("expected same DB handle on second call, got different")
	}
}

func TestStoreFileOperations(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	ns := store.Namespace("power:memory")

	// Write a file
	if err := ns.WriteFile("notes/todo.txt", []byte("buy milk")); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Read it back
	data, err := ns.ReadFile("notes/todo.txt")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "buy milk" {
		t.Errorf("expected 'buy milk', got '%s'", string(data))
	}

	// Write another file
	if err := ns.WriteFile("journal.md", []byte("day 1")); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	// List files (recursive)
	files, err := ns.FileList()
	if err != nil {
		t.Fatalf("file list: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}

	// Verify files are in correct namespace directory
	expectedDir := filepath.Join(dir, "power:memory", "files")
	if _, err := os.Stat(filepath.Join(expectedDir, "notes", "todo.txt")); err != nil {
		t.Errorf("file not in expected location: %v", err)
	}
}

func TestStoreFileIsolation(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	ns1 := store.Namespace("power:memory")
	ns2 := store.Namespace("power:phd")

	// Write to ns1
	if err := ns1.WriteFile("secret.txt", []byte("ns1 data")); err != nil {
		t.Fatalf("write ns1: %v", err)
	}

	// ns2 should NOT be able to read it
	_, err := ns2.ReadFile("secret.txt")
	if err == nil {
		t.Fatal("expected error reading ns1's file from ns2")
	}
}

func TestStorePathTraversal(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	ns := store.Namespace("power:memory")

	// Write a legitimate file first
	if err := ns.WriteFile("legit.txt", []byte("ok")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Try path traversal reads
	traversalPaths := []string{
		"../../etc/passwd",
		"../iam/store.sqlite",
		"/etc/passwd",
		"../../../etc/passwd",
	}

	for _, p := range traversalPaths {
		_, err := ns.ReadFile(p)
		if err == nil {
			t.Errorf("path traversal should have been denied for: %s", p)
		}
	}
}

func TestStoreBackup(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := NewSQLiteStore(dir, logger)
	defer store.Close()

	// Open a namespace and write data
	ns := store.Namespace("iam")
	db, err := ns.DB()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("CREATE TABLE test (id INTEGER)")
	db.Exec("INSERT INTO test VALUES (1)")

	// Backup should checkpoint without error
	if err := store.Backup(context.Background()); err != nil {
		t.Fatalf("backup failed: %v", err)
	}
}
