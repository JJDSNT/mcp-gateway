package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultToolTimeout    = 30 * time.Second
	DefaultMaxConcurrent  = 1
	MaxAllowedConcurrency = 32 // proteção contra configs absurdas

	// Hardening defaults (somente container)
	DefaultDockerNetwork = "none" // "none" | "bridge"
	DefaultReadOnly      = true
)

type Tool struct {
	// Execução
	Runtime string `yaml:"runtime"` // native | container
	Mode    string `yaml:"mode"`    // launcher | daemon (daemon reservado)

	// Native
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`

	// Container
	Image string `yaml:"image"`

	// Limites
	TimeoutMS     int `yaml:"timeout_ms"`     // opcional; se 0 usa default
	MaxConcurrent int `yaml:"max_concurrent"` // opcional; se 0 usa default

	// Hardening (somente container)
	// docker_network: none | bridge (default: none)
	DockerNetwork string `yaml:"docker_network"`
	// read_only: true|false (default: true quando omitido)
	// ponteiro permite distinguir "omitido" de "false"
	ReadOnly *bool `yaml:"read_only"`
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
			// valida hardening específico do container
			if t.DockerNetwork != "" && t.DockerNetwork != "none" && t.DockerNetwork != "bridge" {
				return fmt.Errorf("config: tools[%s].docker_network must be none or bridge", name)
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
	if t.MaxConcurrent > MaxAllowedConcurrency {
		return MaxAllowedConcurrency
	}
	return t.MaxConcurrent
}

// DockerNetworkEffective retorna o modo efetivo de rede para container.
// Default conservador: "none".
func (t Tool) DockerNetworkEffective() string {
	if t.DockerNetwork == "" {
		return DefaultDockerNetwork
	}
	switch t.DockerNetwork {
	case "none", "bridge":
		return t.DockerNetwork
	default:
		return DefaultDockerNetwork
	}
}

// ReadOnlyEffective retorna se o container deve rodar read-only.
// Default conservador: true (quando omitido).
func (t Tool) ReadOnlyEffective() bool {
	if t.ReadOnly == nil {
		return DefaultReadOnly
	}
	return *t.ReadOnly
}
