package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPRoutePreservesPrompt(t *testing.T) {
	prompt := "fix `echo $(touch /bad)` and 'quoted' text\nnext line"
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"route","arguments":{"prompt":` + mustJSON(t, prompt) + `}}}` + "\n"
	var out bytes.Buffer
	var got string
	err := serveMCPWithRun(strings.NewReader(in), &out, func(p string) (string, error) {
		got = p
		return "engine output", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != prompt {
		t.Fatalf("prompt changed: %q", got)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "engine output") {
		t.Fatalf("unexpected MCP response: %s", out.String())
	}
}

func mustJSON(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
