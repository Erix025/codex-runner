package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"codex-runner/internal/shared/miniyaml"
	"codex-runner/internal/shared/osutil"
)

type Project struct {
	ID        string `yaml:"id" json:"id"`
	RepoURL   string `yaml:"repo_url" json:"repo_url"`
	MirrorDir string `yaml:"mirror_dir" json:"mirror_dir"`
}

type Config struct {
	Listen          string    `yaml:"listen" json:"listen"`
	DataDir         string    `yaml:"data_dir" json:"data_dir"`
	AuthToken       string    `yaml:"auth_token" json:"-"`
	RetentionCount  int       `yaml:"retention_count" json:"retention_count"`
	AllowedCwdRoots []string  `yaml:"allowed_cwd_roots" json:"allowed_cwd_roots"`
	Projects        []Project `yaml:"projects" json:"projects"`
}

func Default() Config {
	return Config{
		Listen:         "127.0.0.1:7337",
		DataDir:        "~/.codexd",
		RetentionCount: 200,
	}
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	trim := strings.TrimSpace(string(b))
	if strings.HasPrefix(trim, "{") {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, err
		}
	} else {
		node, err := miniyaml.Parse(strings.NewReader(string(b)))
		if err != nil {
			return Config{}, err
		}
		if err := applyMiniYAML(&cfg, node); err != nil {
			return Config{}, err
		}
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:7337"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "~/.codexd"
	}
	dataDir, err := osutil.ExpandUser(cfg.DataDir)
	if err != nil {
		return Config{}, err
	}
	cfg.DataDir = filepath.Clean(dataDir)
	if cfg.RetentionCount <= 0 {
		cfg.RetentionCount = 200
	}
	for i := range cfg.AllowedCwdRoots {
		p, err := osutil.ExpandUser(cfg.AllowedCwdRoots[i])
		if err != nil {
			return Config{}, err
		}
		cfg.AllowedCwdRoots[i] = filepath.Clean(p)
	}
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		if p.ID == "" {
			return Config{}, errors.New("project id is required")
		}
		if p.RepoURL == "" {
			return Config{}, errors.New("project repo_url is required")
		}
		if p.MirrorDir != "" {
			expanded, err := osutil.ExpandUser(p.MirrorDir)
			if err != nil {
				return Config{}, err
			}
			p.MirrorDir = filepath.Clean(expanded)
		}
	}
	return cfg, nil
}

func applyMiniYAML(cfg *Config, n miniyaml.Node) error {
	if v, ok := n["listen"]; ok {
		cfg.Listen, _ = v.(string)
	}
	if v, ok := n["data_dir"]; ok {
		cfg.DataDir, _ = v.(string)
	}
	if v, ok := n["auth_token"]; ok {
		cfg.AuthToken, _ = v.(string)
	}
	if v, ok := n["retention_count"]; ok {
		switch t := v.(type) {
		case int:
			cfg.RetentionCount = t
		case string:
			// ignore
		}
	}
	if v, ok := n["allowed_cwd_roots"]; ok {
		if arr, ok := v.([]any); ok {
			var out []string
			for _, it := range arr {
				if s, ok := it.(string); ok {
					out = append(out, s)
				}
			}
			cfg.AllowedCwdRoots = out
		}
	}
	if v, ok := n["projects"]; ok {
		if arr, ok := v.([]any); ok {
			var out []Project
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				var p Project
				if s, ok := m["id"].(string); ok {
					p.ID = s
				}
				if s, ok := m["repo_url"].(string); ok {
					p.RepoURL = s
				}
				if s, ok := m["mirror_dir"].(string); ok {
					p.MirrorDir = s
				}
				out = append(out, p)
			}
			cfg.Projects = out
		}
	}
	return nil
}
