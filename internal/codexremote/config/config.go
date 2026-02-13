package config

import (
	"encoding/json"
	"errors"
	"os"
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
