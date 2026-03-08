package core

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level pipeline configuration.
type Config struct {
	Store     StoreConfig     `yaml:"store"`
	Helmet    HelmetConfig    `yaml:"helmet"`
	Cerebro   CerebroConfig   `yaml:"cerebro"`
	Telepath  TelepathConfig  `yaml:"telepath"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// StoreConfig configures the data directory.
type StoreConfig struct {
	DataDir string `yaml:"data_dir"`
}

// HelmetConfig configures IAM.
type HelmetConfig struct {
	RootResolver    RootResolverConfig `yaml:"root_resolver"`
	PowersDir       string             `yaml:"powers_dir"`
	AgentsDir       string             `yaml:"agents_dir"`
	RequireApproval *bool              `yaml:"require_approval"` // unapproved users are silently ignored (default true)
}

// RootResolverConfig determines how root status is resolved.
type RootResolverConfig struct {
	Type  string   `yaml:"type"`   // "env_match", "user_list", "always_true", "always_false"
	Env   string   `yaml:"env"`    // for type=env_match
	Users []string `yaml:"users"` // for type=user_list
}

// CerebroConfig configures the orchestrator.
type CerebroConfig struct {
	DefaultProvider string                `yaml:"default_provider"`
	MaxTurns        int                   `yaml:"max_turns"`
	MaxAgentDepth   int                   `yaml:"max_agent_depth"`
	Tools           map[string]ToolConfig `yaml:"tools"`
}

// ToolConfig is an MCP tool server configuration.
type ToolConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// TelepathConfig configures message sources.
type TelepathConfig struct {
	Sources []SourceConfig `yaml:"sources"`
}

// SourceConfig configures a single message source.
type SourceConfig struct {
	Type       string `yaml:"type"`        // "telegram", "cli"
	ID         string `yaml:"id"`          // "telegram:rogue", "cli:main"
	Token      string `yaml:"token"`       // for telegram
	Agent      string `yaml:"agent"`       // agent ID to route to
	DebounceMS int    `yaml:"debounce_ms"` // for telegram
}

// SchedulerConfig configures the scheduler.
type SchedulerConfig struct {
	TickInterval string `yaml:"tick_interval"` // e.g. "30s"
	TaskTimeout  string `yaml:"task_timeout"`  // e.g. "10m"
	DefaultQueue string `yaml:"default_queue"` // e.g. "review"
}

// LoadConfig loads a YAML config file with env substitution.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Resolve env: references in the raw YAML
	resolved := resolveEnvInYAML(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(resolved), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	if cfg.Cerebro.MaxTurns == 0 {
		cfg.Cerebro.MaxTurns = 100
	}
	if cfg.Cerebro.MaxAgentDepth == 0 {
		cfg.Cerebro.MaxAgentDepth = 3
	}

	return &cfg, nil
}

// resolveEnvInYAML replaces all "env:VAR_NAME" values with the environment variable.
// Also handles ${VAR_NAME} syntax within strings.
func resolveEnvInYAML(content string) string {
	// Handle "env:VAR_NAME" pattern (full value replacement)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Find quoted env: references
		for _, quote := range []string{`"`, `'`} {
			prefix := quote + "env:"
			if idx := strings.Index(line, prefix); idx >= 0 {
				end := strings.Index(line[idx+len(prefix):], quote)
				if end >= 0 {
					varName := line[idx+len(prefix) : idx+len(prefix)+end]
					envVal := os.Getenv(varName)
					old := quote + "env:" + varName + quote
					lines[i] = strings.Replace(lines[i], old, quote+envVal+quote, 1)
				}
			}
		}

		// Handle unquoted env: references (value after colon)
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, ": env:") {
			parts := strings.SplitN(line, ": env:", 2)
			if len(parts) == 2 {
				varName := strings.TrimSpace(parts[1])
				envVal := os.Getenv(varName)
				lines[i] = parts[0] + ": " + quote(envVal)
			}
		}

		// Handle ${VAR_NAME} within strings
		for strings.Contains(lines[i], "${") {
			start := strings.Index(lines[i], "${")
			end := strings.Index(lines[i][start:], "}")
			if end < 0 {
				break
			}
			varName := lines[i][start+2 : start+end]
			envVal := os.Getenv(varName)
			lines[i] = lines[i][:start] + envVal + lines[i][start+end+1:]
		}
	}
	return strings.Join(lines, "\n")
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	// Quote if contains special YAML characters
	if strings.ContainsAny(s, ": #[]{}|>!&*?") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// BuildRootResolver creates a RootResolver from config.
func BuildRootResolver(cfg RootResolverConfig) RootResolver {
	switch cfg.Type {
	case "env_match":
		ownerID := os.Getenv(cfg.Env)
		return func(userID string) bool {
			return ownerID != "" && userID == ownerID
		}
	case "user_list":
		users := make(map[string]bool)
		for _, u := range cfg.Users {
			users[u] = true
		}
		return func(userID string) bool {
			return users[userID]
		}
	case "always_true":
		return func(string) bool { return true }
	case "always_false":
		return func(string) bool { return false }
	default:
		return func(string) bool { return false }
	}
}
