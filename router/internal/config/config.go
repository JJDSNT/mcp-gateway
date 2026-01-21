package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Tool struct {
	Runtime string   `yaml:"runtime"` // native | container
	Mode    string   `yaml:"mode"`    // launcher | daemon (daemon reservado)
	Cmd     string   `yaml:"cmd"`
	Image   string   `yaml:"image"`
	Args    []string `yaml:"args"`
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
	}

	return nil
}
