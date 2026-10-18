package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStarterAndInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := Init(path, false); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Routing.SafeTarget != "claude-opus" || len(c.Targets) != 2 {
		t.Fatalf("bad starter: %+v", c)
	}
	for _, target := range c.Targets {
		for _, effort := range target.Efforts {
			if target.Engine == "claude" && effort == "none" {
				t.Fatalf("starter gives unsupported effort to Claude: %+v", target)
			}
		}
	}
	if err := Init(path, false); !os.IsExist(err) {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	b, _ := os.ReadFile(path)
	expected := Starter
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		expected = strings.Replace(expected, "\nbackend = \"jev\"", "\nbackend = \"heuristics\"", 1)
	}
	if string(b) != expected {
		t.Fatal("existing file changed")
	}
	if err := Init(path, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(Starter, "TYPESAFE_API_KEY =") {
		t.Fatal("starter embeds credentials")
	}
}
func TestValidation(t *testing.T) {
	cases := []struct{ name, find, replacement, want string }{
		{"backend", "\nbackend = \"jev\"", "\nbackend = \"wrong\"", "classifier.backend"},
		{"safe target", "safe_target = \"claude-opus\"", "safe_target = \"missing\"", "routing.safe_target"},
		{"efforts", "efforts = [\"low\", \"medium\"]", "efforts = []", "efforts"},
		{"claude none", "efforts = [\"low\", \"medium\"]", "efforts = [\"none\", \"low\"]", "claude does not support effort"},
		{"one Jev effort", "efforts = [\"medium\", \"high\", \"xhigh\", \"max\"]", "efforts = [\"low\"]", "two distinct effort levels"},
		{"duplicate", "id = \"claude-haiku\"", "id = \"claude-opus\"", "duplicate"},
		{"unknown", "preview_bytes = 8000", "preview_bytes = 8000\nwrong = true", "unknown config key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			s := strings.Replace(Starter, tc.find, tc.replacement, 1)
			if tc.name == "one Jev effort" {
				s = strings.Replace(s, "efforts = [\"low\", \"medium\"]", "efforts = [\"low\"]", 1)
			}
			if err := os.WriteFile(path, []byte(s), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %s, got %v", tc.want, err)
			}
		})
	}
}

func TestInitWithoutKeyUsesOfflineClassifier(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Init(path, false); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Classifier.Backend != "heuristics" {
		t.Fatalf("backend = %s; want heuristics", c.Classifier.Backend)
	}
}
