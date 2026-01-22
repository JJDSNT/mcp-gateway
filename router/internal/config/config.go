package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultToolTimeout      = 30 * time.Second
	DefaultMaxConcurrent    = 1
	MaxAllowedConcurrency   = 32 // proteção contra configs absurdas
)

type Tool struct {
	// Execução
	Runtime string   `yaml:"runtime"` // native | container
	Mode    string   `yaml:"mode"`    // launcher | daemon (daemon reservado)

	// Native
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`

	// Container
	Image string `yaml:"image"`

	// Limites
	TimeoutMS     int `yaml:"timeout_ms"`     // opcional; se 0 usa default
	MaxConcurrent int `yaml:"max_concurrent"` // opcional; se 0 usa default
}

type Config struct {
	WorkspaceRoot string          `yaml:"workspace_root"`
	ToolsRoot     string          `yaml:"tools_root"`
	Tools         map[string]Tool `yaml:"tools"`
}

func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid yaml %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.WorkspaceRoot == "" {
		return fmt.Errorf("config: workspace_root is required")
	}

	if c.ToolsRoot == "" {
		return fmt.Errorf("config: tools_root is required")
	}

	if len(c.Tools) == 0 {
		return fmt.Errorf("config: tools must not be empty")
	}

	for name, t := range c.Tools {
		switch t.Runtime {
		case "native":
			if t.Cmd == "" {
				return fmt.Errorf("config: tools[%s].cmd is required for native runtime", name)
			}
		case "container":
			if t.Image == "" {
				return fmt.Errorf("config: tools[%s].image is required for container runtime", name)
			}
		default:
			return fmt.Errorf("config: tools[%s].runtime must be native or container", name)
		}

		if t.Mode != "" && t.Mode != "launcher" && t.Mode != "daemon" {
			return fmt.Errorf("config: tools[%s].mode must be launcher or daemon", name)
		}

		if t.TimeoutMS < 0 {
			return fmt.Errorf("config: tools[%s].timeout_ms must be >= 0", name)
		}

		if t.MaxConcurrent < 0 {
			return fmt.Errorf("config: tools[%s].max_concurrent must be >= 0", name)
		}

		if t.MaxConcurrent > MaxAllowedConcurrency {
			return fmt.Errorf(
				"config: tools[%s].max_concurrent must be <= %d",
				name,
				MaxAllowedConcurrency,
			)
		}
	}

	return nil
}

// Timeout retorna o timeout efetivo da tool.
// Invariante do core: NENHUMA tool roda sem timeout.
func (t Tool) Timeout() time.Duration {
	if t.TimeoutMS <= 0 {
		return DefaultToolTimeout
	}
	return time.Duration(t.TimeoutMS) * time.Millisecond
}

// MaxConc retorna o limite efetivo de concorrência da tool.
// Default conservador para evitar fork-bomb acidental.
func (t Tool) MaxConc() int {
	if t.MaxConcurrent <= 0 {
		return DefaultMaxConcurrent
	}
	return t.MaxConcurrent
}
