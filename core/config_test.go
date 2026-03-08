package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigBasic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	os.WriteFile(configPath, []byte(`
store:
  data_dir: /tmp/rogue-test

helmet:
  root_resolver:
    type: always_true
  powers_dir: ./powers
  agents_dir: ./agents

cerebro:
  default_provider: claude-code
  max_turns: 50
  max_agent_depth: 5

telepath:
  sources:
    - type: cli
      id: "cli:main"
      agent: rogue

scheduler:
  tick_interval: "30s"
  default_queue: main
`), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Store.DataDir != "/tmp/rogue-test" {
		t.Errorf("expected data_dir /tmp/rogue-test, got %s", cfg.Store.DataDir)
	}
	if cfg.Helmet.RootResolver.Type != "always_true" {
		t.Errorf("expected root_resolver type always_true, got %s", cfg.Helmet.RootResolver.Type)
	}
	if cfg.Helmet.PowersDir != "./powers" {
		t.Errorf("expected powers_dir ./powers, got %s", cfg.Helmet.PowersDir)
	}
	if cfg.Cerebro.MaxTurns != 50 {
		t.Errorf("expected max_turns 50, got %d", cfg.Cerebro.MaxTurns)
	}
	if cfg.Cerebro.MaxAgentDepth != 5 {
		t.Errorf("expected max_agent_depth 5, got %d", cfg.Cerebro.MaxAgentDepth)
	}
	if len(cfg.Telepath.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(cfg.Telepath.Sources))
	}
	if cfg.Telepath.Sources[0].Type != "cli" {
		t.Errorf("expected source type cli, got %s", cfg.Telepath.Sources[0].Type)
	}
	if cfg.Scheduler.TickInterval != "30s" {
		t.Errorf("expected tick_interval 30s, got %s", cfg.Scheduler.TickInterval)
	}
}

func TestLoadConfigEnvSubstitution(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	t.Setenv("TEST_ROGUE_DATA", "/data/rogue")
	t.Setenv("TEST_BOT_TOKEN", "12345:ABCDEF")
	t.Setenv("TEST_OWNER_ID", "99999")

	os.WriteFile(configPath, []byte(`
store:
  data_dir: "env:TEST_ROGUE_DATA"

helmet:
  root_resolver:
    type: env_match
    env: TEST_OWNER_ID

telepath:
  sources:
    - type: telegram
      id: "telegram:rogue"
      token: "env:TEST_BOT_TOKEN"
      agent: rogue
      debounce_ms: 5000
`), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Store.DataDir != "/data/rogue" {
		t.Errorf("expected /data/rogue, got %s", cfg.Store.DataDir)
	}
	if cfg.Telepath.Sources[0].Token != "12345:ABCDEF" {
		t.Errorf("expected token 12345:ABCDEF, got %s", cfg.Telepath.Sources[0].Token)
	}
}

func TestLoadConfigDollarSubstitution(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	t.Setenv("MY_BIN_DIR", "/usr/local/bin")
	t.Setenv("MY_DATA", "/data")

	os.WriteFile(configPath, []byte(`
cerebro:
  tools:
    rogue-store:
      command: "${MY_BIN_DIR}/rogue-store"
      env:
        ROGUE_DATA: "${MY_DATA}"
`), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	tool, ok := cfg.Cerebro.Tools["rogue-store"]
	if !ok {
		t.Fatal("expected rogue-store tool config")
	}
	if tool.Command != "/usr/local/bin/rogue-store" {
		t.Errorf("expected /usr/local/bin/rogue-store, got %s", tool.Command)
	}
	if tool.Env["ROGUE_DATA"] != "/data" {
		t.Errorf("expected ROGUE_DATA=/data, got %s", tool.Env["ROGUE_DATA"])
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	os.WriteFile(configPath, []byte(`
store:
  data_dir: /tmp
`), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Cerebro.MaxTurns != 100 {
		t.Errorf("expected default max_turns 100, got %d", cfg.Cerebro.MaxTurns)
	}
	if cfg.Cerebro.MaxAgentDepth != 3 {
		t.Errorf("expected default max_agent_depth 3, got %d", cfg.Cerebro.MaxAgentDepth)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestBuildRootResolverEnvMatch(t *testing.T) {
	t.Setenv("TEST_OWNER", "user-42")

	resolver := BuildRootResolver(RootResolverConfig{
		Type: "env_match",
		Env:  "TEST_OWNER",
	})

	if !resolver("user-42") {
		t.Error("user-42 should be root")
	}
	if resolver("user-99") {
		t.Error("user-99 should not be root")
	}
}

func TestBuildRootResolverUserList(t *testing.T) {
	resolver := BuildRootResolver(RootResolverConfig{
		Type:  "user_list",
		Users: []string{"alice", "bob"},
	})

	if !resolver("alice") {
		t.Error("alice should be root")
	}
	if !resolver("bob") {
		t.Error("bob should be root")
	}
	if resolver("charlie") {
		t.Error("charlie should not be root")
	}
}

func TestBuildRootResolverAlwaysTrue(t *testing.T) {
	resolver := BuildRootResolver(RootResolverConfig{Type: "always_true"})
	if !resolver("anyone") {
		t.Error("always_true should return true")
	}
}

func TestBuildRootResolverAlwaysFalse(t *testing.T) {
	resolver := BuildRootResolver(RootResolverConfig{Type: "always_false"})
	if resolver("anyone") {
		t.Error("always_false should return false")
	}
}

func TestBuildRootResolverUnknownType(t *testing.T) {
	resolver := BuildRootResolver(RootResolverConfig{Type: "unknown"})
	if resolver("anyone") {
		t.Error("unknown type should default to false")
	}
}

func TestLoadConfigWithToolsMap(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	os.WriteFile(configPath, []byte(`
cerebro:
  default_provider: claude-code
  max_turns: 100
  tools:
    rogue-store:
      command: /bin/rogue-store
      env:
        ROGUE_DATA: /data
    rogue-scheduler:
      command: /bin/rogue-scheduler
      args:
        - --mode
        - mcp
`), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if len(cfg.Cerebro.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(cfg.Cerebro.Tools))
	}

	store := cfg.Cerebro.Tools["rogue-store"]
	if store.Command != "/bin/rogue-store" {
		t.Errorf("expected /bin/rogue-store, got %s", store.Command)
	}
	if store.Env["ROGUE_DATA"] != "/data" {
		t.Errorf("expected ROGUE_DATA=/data")
	}

	sched := cfg.Cerebro.Tools["rogue-scheduler"]
	if len(sched.Args) != 2 || sched.Args[0] != "--mode" {
		t.Errorf("expected args [--mode mcp], got %v", sched.Args)
	}
}
