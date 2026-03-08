package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type defaultHelmet struct {
	store           Store
	rootResolver    RootResolver
	requireApproval bool
	powersDir       string
	agentsDir       string
	powerCache      map[string]*Power
	powerMu         sync.RWMutex
	logger          *slog.Logger
}

func NewHelmet(store Store, rootResolver RootResolver, logger *slog.Logger, opts ...HelmetOption) Helmet {
	h := &defaultHelmet{
		store:        store,
		rootResolver: rootResolver,
		powersDir:    "powers",
		agentsDir:    "agents",
		powerCache:   make(map[string]*Power),
		logger:       logger,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type HelmetOption func(*defaultHelmet)

func WithPowersDir(dir string) HelmetOption {
	return func(h *defaultHelmet) { h.powersDir = dir }
}

func WithAgentsDir(dir string) HelmetOption {
	return func(h *defaultHelmet) { h.agentsDir = dir }
}

func WithRequireApproval(require bool) HelmetOption {
	return func(h *defaultHelmet) { h.requireApproval = require }
}

func (h *defaultHelmet) Process(ctx context.Context, msg Message) (*EnrichedMessage, error) {
	h.logger.Info("processing message",
		"source_id", msg.SourceID,
		"user_id", msg.UserID,
		"channel_id", msg.ChannelID,
		"agent_id", msg.AgentID,
	)

	db, err := h.store.Namespace("iam").DB()
	if err != nil {
		return nil, fmt.Errorf("failed to open IAM database: %w", err)
	}

	// Ensure schema
	if err := h.ensureSchema(db); err != nil {
		return nil, fmt.Errorf("schema migration failed: %w", err)
	}

	// 1. Get or create user
	user, err := h.getOrCreateUser(db, msg.UserID, msg.Metadata)
	if err != nil {
		return nil, fmt.Errorf("user resolution failed: %w", err)
	}

	// 2. Map (source, channel, agent) -> chat_id
	chatID, err := h.getOrCreateChat(db, msg.SourceID, msg.ChannelID, msg.AgentID)
	if err != nil {
		return nil, fmt.Errorf("chat resolution failed: %w", err)
	}

	// 3. Get or create session
	session := Session{
		ID:        chatID,
		ChannelID: msg.ChannelID,
		AgentID:   msg.AgentID,
		SourceID:  msg.SourceID,
	}

	// 4. Resolve root status
	isRoot := h.rootResolver(msg.UserID)

	// 5. Load agent persona
	agent := h.loadAgent(msg.AgentID)

	// 6. Resolve powers and build powerset
	powerSet, err := h.resolvePowerSet(db, msg.AgentID, msg.UserID, msg.ChannelID, isRoot)
	if err != nil {
		h.logger.Error("power resolution failed", "error", err)
		powerSet = PowerSet{}
	}

	// 7. Build tags (auto-generated + admin-configured)
	tags := h.buildTags(db, msg, chatID)

	// Determine effective approval: root is always approved
	approved := user.Approved || isRoot

	enriched := &EnrichedMessage{
		Message:  msg,
		ChatID:   chatID,
		Session:  session,
		User:     *user,
		Agent:    agent,
		PowerSet: powerSet,
		Tags:     tags,
		IsRoot:   isRoot,
		Approved: approved,
	}

	h.logger.Info("message enriched",
		"chat_id", chatID,
		"is_root", isRoot,
		"powers", len(powerSet.Powers),
		"tools", len(powerSet.Tools),
		"tags", tags,
	)

	return enriched, nil
}

// --- Schema ---

func (h *defaultHelmet) ensureSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT DEFAULT '',
			first_name TEXT DEFAULT '',
			approved BOOLEAN DEFAULT 0,
			created_at DATETIME DEFAULT (datetime('now')),
			last_seen DATETIME DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS chats (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			created_at DATETIME DEFAULT (datetime('now')),
			UNIQUE(source_id, channel_id, agent_id)
		);
		CREATE TABLE IF NOT EXISTS user_powers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			power_name TEXT NOT NULL,
			assigned_by TEXT DEFAULT '',
			assigned_at DATETIME DEFAULT (datetime('now')),
			UNIQUE(agent_id, user_id, channel_id, power_name)
		);
		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			UNIQUE(chat_id, tag)
		);
		CREATE TABLE IF NOT EXISTS usage_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT (datetime('now')),
			user_id TEXT,
			channel_id TEXT,
			agent_id TEXT,
			chat_id TEXT,
			source_id TEXT,
			message_type TEXT DEFAULT 'message',
			powers TEXT DEFAULT '',
			tags TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0,
			cache_write_tokens INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			duration_ms INTEGER DEFAULT 0,
			num_turns INTEGER DEFAULT 0,
			hit_max_turns BOOLEAN DEFAULT 0,
			session_state TEXT DEFAULT ''
		);
	`)
	return err
}

// --- User Management ---

func (h *defaultHelmet) getOrCreateUser(db *sql.DB, userID string, metadata map[string]any) (*User, error) {
	// Migration: add approved column if missing (for existing DBs)
	db.Exec("ALTER TABLE users ADD COLUMN approved BOOLEAN DEFAULT 0")

	var user User
	err := db.QueryRow("SELECT id, username, first_name, approved, created_at, last_seen FROM users WHERE id = ?", userID).
		Scan(&user.ID, &user.Username, &user.FirstName, &user.Approved, &user.CreatedAt, &user.LastSeen)

	if err == sql.ErrNoRows {
		username := ""
		firstName := ""
		if metadata != nil {
			if v, ok := metadata["username"].(string); ok {
				username = v
			}
			if v, ok := metadata["first_name"].(string); ok {
				firstName = v
			}
		}

		now := time.Now()
		_, err = db.Exec("INSERT INTO users (id, username, first_name, approved, created_at, last_seen) VALUES (?, ?, ?, ?, ?, ?)",
			userID, username, firstName, false, now, now)
		if err != nil {
			return nil, err
		}

		user = User{ID: userID, Username: username, FirstName: firstName, Approved: false, CreatedAt: now, LastSeen: now}
		h.logger.Info("user created", "user_id", userID, "username", username)
	} else if err != nil {
		return nil, err
	} else {
		// Update last_seen
		db.Exec("UPDATE users SET last_seen = datetime('now') WHERE id = ?", userID)
	}

	return &user, nil
}

// --- Chat (Session) Mapping ---

func (h *defaultHelmet) getOrCreateChat(db *sql.DB, sourceID, channelID, agentID string) (string, error) {
	var chatID string
	err := db.QueryRow("SELECT id FROM chats WHERE source_id = ? AND channel_id = ? AND agent_id = ?",
		sourceID, channelID, agentID).Scan(&chatID)

	if err == sql.ErrNoRows {
		chatID = generateID()
		_, err = db.Exec("INSERT INTO chats (id, source_id, channel_id, agent_id) VALUES (?, ?, ?, ?)",
			chatID, sourceID, channelID, agentID)
		if err != nil {
			return "", err
		}
		h.logger.Info("chat created", "chat_id", chatID, "source", sourceID, "channel", channelID, "agent", agentID)
	} else if err != nil {
		return "", err
	}

	return chatID, nil
}

// --- Agent Persona ---

func (h *defaultHelmet) loadAgent(agentID string) AgentConfig {
	path := filepath.Join(h.agentsDir, agentID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		h.logger.Info("no persona file", "agent_id", agentID, "path", path)
		return AgentConfig{ID: agentID, Persona: ""}
	}

	now := time.Now()
	timeContext := fmt.Sprintf("\n\n## Current Time Context\n\nCurrent datetime: %s\nTimezone: %s\nWeekday: %s",
		now.Format(time.RFC3339),
		now.Location().String(),
		now.Weekday().String())

	return AgentConfig{
		ID:      agentID,
		Persona: string(data) + timeContext,
	}
}

// --- Power Resolution ---

type powerFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Namespace   string   `yaml:"namespace"`
	Tools       []string `yaml:"tools"`
	Directories []string `yaml:"directories"`
}

func (h *defaultHelmet) loadPower(name string) (*Power, error) {
	h.powerMu.RLock()
	if p, ok := h.powerCache[name]; ok {
		h.powerMu.RUnlock()
		return p, nil
	}
	h.powerMu.RUnlock()

	path := filepath.Join(h.powersDir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("power file not found: %s", name)
	}

	content := string(data)
	var fm powerFrontmatter
	var instructions string

	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content[3:], "---", 2)
		if len(parts) == 2 {
			if err := yaml.Unmarshal([]byte(parts[0]), &fm); err != nil {
				return nil, fmt.Errorf("invalid frontmatter in %s: %w", name, err)
			}
			instructions = strings.TrimSpace(parts[1])
		}
	}

	if fm.Name == "" {
		fm.Name = name
	}
	if fm.Namespace == "" {
		fm.Namespace = "power:" + name
	}

	power := &Power{
		Name:         fm.Name,
		Namespace:    fm.Namespace,
		Tools:        fm.Tools,
		Directories:  fm.Directories,
		Instructions: instructions,
	}

	h.powerMu.Lock()
	h.powerCache[name] = power
	h.powerMu.Unlock()

	return power, nil
}

func (h *defaultHelmet) resolvePowerSet(db *sql.DB, agentID, userID, channelID string, isRoot bool) (PowerSet, error) {
	rows, err := db.Query(
		"SELECT DISTINCT power_name FROM user_powers WHERE agent_id = ? AND user_id = ? AND channel_id = ?",
		agentID, userID, channelID)
	if err != nil {
		return PowerSet{}, err
	}
	defer rows.Close()

	var powerNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		powerNames = append(powerNames, name)
	}

	// Also check global powers (channel_id = "")
	globalRows, err := db.Query(
		"SELECT DISTINCT power_name FROM user_powers WHERE agent_id = ? AND user_id = ? AND channel_id = ''",
		agentID, userID)
	if err == nil {
		defer globalRows.Close()
		for globalRows.Next() {
			var name string
			if err := globalRows.Scan(&name); err == nil {
				powerNames = append(powerNames, name)
			}
		}
	}

	// Root always gets the "powers" power
	if isRoot {
		powerNames = append(powerNames, "powers")
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, n := range powerNames {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}

	// Load powers and build union
	var powers []Power
	toolSet := make(map[string]bool)
	dirSet := make(map[string]bool)
	var instructions []string

	for _, name := range unique {
		power, err := h.loadPower(name)
		if err != nil {
			h.logger.Warn("failed to load power", "name", name, "error", err)
			continue
		}
		powers = append(powers, *power)
		for _, t := range power.Tools {
			toolSet[t] = true
		}
		for _, d := range power.Directories {
			dirSet[d] = true
		}
		if power.Instructions != "" {
			instructions = append(instructions, fmt.Sprintf("## Power: %s\n\n%s", power.Name, power.Instructions))
		}
	}

	var tools []string
	for t := range toolSet {
		tools = append(tools, t)
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}

	return PowerSet{
		Powers:       powers,
		Tools:        tools,
		Directories:  dirs,
		Instructions: strings.Join(instructions, "\n\n---\n\n"),
	}, nil
}

// --- Tags ---

func (h *defaultHelmet) buildTags(db *sql.DB, msg Message, chatID string) []string {
	// Auto-generated tags
	tags := []string{
		"user:" + msg.UserID,
		"source:" + msg.SourceID,
		"channel:" + msg.ChannelID,
		"agent:" + msg.AgentID,
	}

	// Load admin-configured tags from DB
	rows, err := db.Query("SELECT tag FROM tags WHERE chat_id = ?", chatID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tag string
			if err := rows.Scan(&tag); err == nil {
				tags = append(tags, tag)
			}
		}
	}

	return tags
}

// --- Power Assignment (for use by MCP tools / admin) ---

func (h *defaultHelmet) AssignPower(agentID, userID, channelID, powerName, assignedBy string) error {
	db, err := h.store.Namespace("iam").DB()
	if err != nil {
		return err
	}
	h.ensureSchema(db)

	_, err = db.Exec(`
		INSERT INTO user_powers (agent_id, user_id, channel_id, power_name, assigned_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, user_id, channel_id, power_name)
		DO UPDATE SET assigned_by = excluded.assigned_by, assigned_at = datetime('now')
	`, agentID, userID, channelID, powerName, assignedBy)
	return err
}

func (h *defaultHelmet) RevokePower(agentID, userID, channelID, powerName string) error {
	db, err := h.store.Namespace("iam").DB()
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"DELETE FROM user_powers WHERE agent_id = ? AND user_id = ? AND channel_id = ? AND power_name = ?",
		agentID, userID, channelID, powerName)
	return err
}
