package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kidkuddy/rogue-core/core"
)

var store core.Store

func main() {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "ROGUE_DATA environment variable is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store = core.NewSQLiteStore(dataDir, logger)
	defer store.Close()

	s := server.NewMCPServer("rogue-store", "1.0.0")

	s.AddTool(mcp.NewTool("sql",
		mcp.WithDescription("Execute a SQL query against a namespaced SQLite database. Use for structured data operations."),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Storage namespace (e.g. 'power:memory', 'power:contacts')")),
		mcp.WithString("query", mcp.Required(), mcp.Description("SQL query to execute")),
	), handleSQL)

	s.AddTool(mcp.NewTool("db_list",
		mcp.WithDescription("List all available database namespaces."),
	), handleDBList)

	s.AddTool(mcp.NewTool("file_read",
		mcp.WithDescription("Read a file from a namespace's file storage."),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Storage namespace")),
		mcp.WithString("name", mcp.Required(), mcp.Description("File name/path within namespace")),
	), handleFileRead)

	s.AddTool(mcp.NewTool("file_write",
		mcp.WithDescription("Write a file to a namespace's file storage."),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Storage namespace")),
		mcp.WithString("name", mcp.Required(), mcp.Description("File name/path within namespace")),
		mcp.WithString("content", mcp.Required(), mcp.Description("File content to write")),
	), handleFileWrite)

	s.AddTool(mcp.NewTool("file_list",
		mcp.WithDescription("List all files in a namespace's file storage."),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Storage namespace")),
	), handleFileList)

	s.AddTool(mcp.NewTool("file_delete",
		mcp.WithDescription("Delete a file from a namespace's file storage. Requires a confirmation code from the user."),
		mcp.WithString("namespace", mcp.Required(), mcp.Description("Storage namespace")),
		mcp.WithString("name", mcp.Required(), mcp.Description("File name/path within namespace")),
		mcp.WithString("confirm", mcp.Required(), mcp.Description("User must say 'yes delete' to confirm. Ask them first.")),
	), handleFileDelete)

	s.AddTool(mcp.NewTool("backup",
		mcp.WithDescription("Back up the entire data folder. Checkpoints all databases first, then copies everything to a timestamped backup directory."),
	), handleBackup)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func handleSQL(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace, _ := req.GetArguments()["namespace"].(string)
	query, _ := req.GetArguments()["query"].(string)

	if namespace == "" || query == "" {
		return mcp.NewToolResultError("namespace and query are required"), nil
	}

	db, err := store.Namespace(namespace).DB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to open namespace: %v", err)), nil
	}

	trimmed := strings.TrimSpace(strings.ToUpper(query))
	isSelect := strings.HasPrefix(trimmed, "SELECT") ||
		strings.HasPrefix(trimmed, "PRAGMA") ||
		strings.HasPrefix(trimmed, "EXPLAIN")

	if isSelect {
		return execQuery(db, query)
	}
	return execStatement(db, query)
}

func execQuery(db *sql.DB, query string) (*mcp.CallToolResult, error) {
	rows, err := db.Query(query)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]any

	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan error: %v", err)), nil
		}

		row := make(map[string]any)
		for i, col := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = v
			}
		}
		results = append(results, row)
	}

	if results == nil {
		results = []map[string]any{}
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func execStatement(db *sql.DB, query string) (*mcp.CallToolResult, error) {
	result, err := db.Exec(query)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("exec error: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()

	return mcp.NewToolResultText(fmt.Sprintf("OK. Rows affected: %d, Last ID: %d", affected, lastID)), nil
}

func handleDBList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dataDir := os.Getenv("ROGUE_DATA")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return mcp.NewToolResultText("[]"), nil
	}

	var namespaces []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A namespace has a db/ or files/ subdirectory
		dbPath := fmt.Sprintf("%s/%s/db", dataDir, e.Name())
		filesPath := fmt.Sprintf("%s/%s/files", dataDir, e.Name())
		if dirExists(dbPath) || dirExists(filesPath) {
			namespaces = append(namespaces, e.Name())
		}
	}

	out, _ := json.MarshalIndent(namespaces, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleFileRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace, _ := req.GetArguments()["namespace"].(string)
	name, _ := req.GetArguments()["name"].(string)

	data, err := store.Namespace(namespace).ReadFile(name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func handleFileWrite(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace, _ := req.GetArguments()["namespace"].(string)
	name, _ := req.GetArguments()["name"].(string)
	content, _ := req.GetArguments()["content"].(string)

	if err := store.Namespace(namespace).WriteFile(name, []byte(content)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write error: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Written %d bytes to %s/%s", len(content), namespace, name)), nil
}

func handleFileList(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace, _ := req.GetArguments()["namespace"].(string)

	files, err := store.Namespace(namespace).FileList()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list error: %v", err)), nil
	}

	type fileEntry struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		ModTime string `json:"mod_time"`
	}

	entries := make([]fileEntry, len(files))
	for i, f := range files {
		entries[i] = fileEntry{
			Name:    f.Name,
			Size:    f.Size,
			ModTime: f.ModTime.Format("2006-01-02 15:04:05"),
		}
	}

	out, _ := json.MarshalIndent(entries, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleFileDelete(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace, _ := req.GetArguments()["namespace"].(string)
	name, _ := req.GetArguments()["name"].(string)
	confirm, _ := req.GetArguments()["confirm"].(string)

	if namespace == "" || name == "" {
		return mcp.NewToolResultError("namespace and name are required"), nil
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "yes delete" {
		return mcp.NewToolResultError("deletion not confirmed. Ask the user to say 'yes delete' before calling this tool."), nil
	}

	ns := store.Namespace(namespace)
	path := ns.FilePath(name)

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("file not found: %s/%s", namespace, name)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("delete error: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Deleted %s/%s", namespace, name)), nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func handleBackup(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		return mcp.NewToolResultError("ROGUE_DATA not set"), nil
	}

	// Step 1: WAL checkpoint all databases for consistency
	var checkpointed []string
	var errs []string

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read data dir: %v", err)), nil
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dbPath := filepath.Join(dataDir, e.Name(), "db", "store.sqlite")
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: open failed: %v", e.Name(), err))
			continue
		}
		if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			errs = append(errs, fmt.Sprintf("%s: checkpoint failed: %v", e.Name(), err))
		} else {
			checkpointed = append(checkpointed, e.Name())
		}
		db.Close()
	}

	// Step 2: Copy entire data folder to timestamped backup
	timestamp := time.Now().Format("2006-01-02_150405")
	backupDir := dataDir + "_backup_" + timestamp

	if err := copyDir(dataDir, backupDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("backup copy failed: %v", err)), nil
	}

	result := fmt.Sprintf("Backup complete → %s\nCheckpointed %d databases: %s",
		backupDir, len(checkpointed), strings.Join(checkpointed, ", "))
	if len(errs) > 0 {
		result += fmt.Sprintf("\nCheckpoint errors: %s", strings.Join(errs, "; "))
	}
	return mcp.NewToolResultText(result), nil
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
