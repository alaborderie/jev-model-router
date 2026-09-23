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
	if err := Init(path, false); !os.IsExist(err) {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != Starter {
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
		{"efforts", "efforts = [\"none\", \"low\", \"medium\"]", "efforts = []", "efforts"},
		{"duplicate", "id = \"claude-haiku\"", "id = \"claude-opus\"", "duplicate"},
		{"unknown", "preview_bytes = 8000", "preview_bytes = 8000\nwrong = true", "unknown config key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			s := strings.Replace(Starter, tc.find, tc.replacement, 1)
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
