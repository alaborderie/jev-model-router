package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Classifier struct {
	Backend        string  `toml:"backend"`
	ConfidenceGate float64 `toml:"confidence_gate"`
	TimeoutMS      int     `toml:"timeout_ms"`
	PreviewBytes   int     `toml:"preview_bytes"`
	Jev            Jev     `toml:"jev"`
	OpenAI         OpenAI  `toml:"openai"`
}
type Jev struct {
	Model string `toml:"model"`
}
type OpenAI struct {
	BaseURL string `toml:"base_url"`
	Model   string `toml:"model"`
}
type Routing struct {
	SafeTarget    string `toml:"safe_target"`
	DefaultEngine string `toml:"default_engine"`
}
type Target struct {
	ID      string   `toml:"id"`
	Engine  string   `toml:"engine"`
	Model   string   `toml:"model"`
	Efforts []string `toml:"efforts"`
	When    string   `toml:"when"`
}
type Config struct {
	Classifier Classifier `toml:"classifier"`
	Routing    Routing    `toml:"routing"`
	Targets    []Target   `toml:"target"`
}

const Starter = `# mroute sends at most preview_bytes of a prompt to TypeSafe when backend = "jev".
# Set TYPESAFE_API_KEY in your environment. Use "heuristics" for offline routing.
[classifier]
backend = "jev"
confidence_gate = 0.60
timeout_ms = 2000
preview_bytes = 8000

[classifier.jev]
model = "jev-latest"

[classifier.openai]
base_url = "http://127.0.0.1:8080/v1"
model = "local"

[routing]
safe_target = "claude-opus"
default_engine = "claude"

[[target]]
id = "claude-haiku"
engine = "claude"
model = "claude-haiku-4-5"
efforts = ["none", "low", "medium"]
when = "mechanical edits, renames, formatting, single-file lookups"

[[target]]
id = "claude-opus"
engine = "claude"
model = "claude-opus-4-6"
efforts = ["medium", "high", "xhigh", "max"]
when = "debugging, architecture, multi-file refactors, ambiguous tasks"
`

func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "mroute", "config.toml"), nil
}
func Init(path string, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(Starter)
	return err
}
func Load(path string) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return c, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return c, fmt.Errorf("unknown config key %s", undecoded[0])
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	switch c.Classifier.Backend {
	case "jev", "openai", "heuristics":
	default:
		return fmt.Errorf("classifier.backend must be jev, openai, or heuristics (got %q)", c.Classifier.Backend)
	}
	if c.Classifier.ConfidenceGate < 0 || c.Classifier.ConfidenceGate > 1 {
		return errors.New("classifier.confidence_gate must be between 0 and 1")
	}
	if c.Classifier.TimeoutMS <= 0 {
		return errors.New("classifier.timeout_ms must be positive")
	}
	if c.Classifier.PreviewBytes <= 0 || c.Classifier.PreviewBytes > 32768 {
		return errors.New("classifier.preview_bytes must be between 1 and 32768")
	}
	if c.Classifier.Backend == "jev" && c.Classifier.Jev.Model == "" {
		return errors.New("classifier.jev.model is required")
	}
	if c.Classifier.Backend == "openai" && (c.Classifier.OpenAI.BaseURL == "" || c.Classifier.OpenAI.Model == "") {
		return errors.New("classifier.openai.base_url and model are required")
	}
	if len(c.Targets) < 2 {
		return errors.New("at least two [[target]] entries are required")
	}
	ids := map[string]bool{}
	validEffort := map[string]bool{"none": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	for i, t := range c.Targets {
		prefix := fmt.Sprintf("target[%d]", i)
		if t.ID == "" || t.Model == "" || strings.TrimSpace(t.When) == "" {
			return fmt.Errorf("%s needs id, model, and when", prefix)
		}
		if ids[t.ID] {
			return fmt.Errorf("duplicate target id %q", t.ID)
		}
		ids[t.ID] = true
		switch t.Engine {
		case "claude", "opencode", "codex":
		default:
			return fmt.Errorf("%s.engine must be claude, opencode, or codex", prefix)
		}
		if len(t.Efforts) == 0 {
			return fmt.Errorf("%s.efforts must not be empty", prefix)
		}
		seen := map[string]bool{}
		for _, e := range t.Efforts {
			if !validEffort[e] || seen[e] {
				return fmt.Errorf("%s.efforts contains invalid or duplicate effort %q", prefix, e)
			}
			seen[e] = true
		}
	}
	if !ids[c.Routing.SafeTarget] {
		return fmt.Errorf("routing.safe_target %q must name a configured target", c.Routing.SafeTarget)
	}
	switch c.Routing.DefaultEngine {
	case "claude", "opencode", "codex":
	default:
		return errors.New("routing.default_engine must be claude, opencode, or codex")
	}
	return nil
}
func (c Config) Target(id string) (Target, bool) {
	for _, t := range c.Targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}
