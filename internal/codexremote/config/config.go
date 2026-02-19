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

type Machine struct {
	Name          string `yaml:"name" json:"name"`
	Addr          string `yaml:"addr" json:"addr"`
	SSH           string `yaml:"ssh" json:"ssh"`
	Token         string `yaml:"token" json:"-"`
	DaemonPort    int    `yaml:"daemon_port" json:"daemon_port"`
	DaemonCmd     string `yaml:"daemon_cmd" json:"daemon_cmd"`
	UseDirectAddr bool   `yaml:"use_direct_addr" json:"use_direct_addr"`
}

type Config struct {
	Machines []Machine `yaml:"machines" json:"machines"`
}

func Default() Config {
	return Config{}
}

const defaultConfigTemplate = `# Local config (YAML subset).
machines:
  - name: gpu1
    # Option A (recommended): SSH target, codex-remote will create a temporary local port-forward per command.
    ssh: user@gpu1.example.com
    daemon_port: 7337

    # Option B: direct addr (e.g. you already have VSCode port-forward to localhost:7337)
    # addr: http://127.0.0.1:7337

    # Optional: token if codexd has auth_token configured
    # token: change-me

    # Optional: enable explicit ssh -f -N -L tunnel + direct addr path for exec start.
    # use_direct_addr: true

    # Optional: how to start codexd via SSH (used by machine up / dashboard Up button)
    # daemon_cmd: "nohup ~/bin/codexd serve --config ~/.codexd/config.yaml >/tmp/codexd.log 2>&1 &"
`

func EnsureDefaultConfig(path string) (created bool, resolvedPath string, err error) {
	p, err := osutil.ExpandUser(path)
	if err != nil {
		return false, "", err
	}
	if _, err := os.Stat(p); err == nil {
		return false, p, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, "", err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, p, nil
		}
		return false, "", err
	}
	defer f.Close()
	if _, err := f.WriteString(defaultConfigTemplate); err != nil {
		return false, "", err
	}
	return true, p, nil
}

func Load(path string) (Config, error) {
	p, err := osutil.ExpandUser(path)
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
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
	for i := range cfg.Machines {
		m := &cfg.Machines[i]
		if m.Name == "" {
			return Config{}, errors.New("machine.name is required")
		}
		if m.DaemonPort == 0 {
			m.DaemonPort = 7337
		}
		if m.DaemonCmd == "" {
			m.DaemonCmd = "nohup codexd serve --config ~/.codexd/config.yaml >/tmp/codexd.log 2>&1 &"
		}
		// Normalize addr if provided without scheme.
		if m.Addr != "" && !(hasPrefix(m.Addr, "http://") || hasPrefix(m.Addr, "https://")) {
			m.Addr = "http://" + m.Addr
		}
	}
	return cfg, nil
}

func (c Config) FindMachine(name string) (*Machine, bool) {
	for i := range c.Machines {
		if c.Machines[i].Name == name {
			return &c.Machines[i], true
		}
	}
	return nil, false
}

func hasPrefix(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}

func applyMiniYAML(cfg *Config, n miniyaml.Node) error {
	v, ok := n["machines"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []Machine
	for _, it := range arr {
		mm, ok := it.(map[string]any)
		if !ok {
			continue
		}
		var m Machine
		if s, ok := mm["name"].(string); ok {
			m.Name = s
		}
		if s, ok := mm["addr"].(string); ok {
			m.Addr = s
		}
		if s, ok := mm["ssh"].(string); ok {
			m.SSH = s
		}
		if s, ok := mm["token"].(string); ok {
			m.Token = s
		}
		if s, ok := mm["daemon_cmd"].(string); ok {
			m.DaemonCmd = s
		}
		if b, ok := asBool(mm["use_direct_addr"]); ok {
			m.UseDirectAddr = b
		}
		if p, ok := mm["daemon_port"]; ok {
			if i, ok := p.(int); ok {
				m.DaemonPort = i
			}
		}
		out = append(out, m)
	}
	cfg.Machines = out
	return nil
}

func asBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		switch s {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}
