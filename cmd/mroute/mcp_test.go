package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPRoutePreservesPrompt(t *testing.T) {
	prompt := " \tfix `echo $(touch /bad)` and 'quoted' text\nnext line \n"
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

func TestMCPRouteRejectsWhitespaceOnlyPrompt(t *testing.T) {
	for _, prompt := range []string{"", "   ", "\t\t", "\n\r\t  \n"} {
		t.Run(mustJSON(t, prompt), func(t *testing.T) {
			in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"route","arguments":{"prompt":` + mustJSON(t, prompt) + `}}}` + "\n"
			var out bytes.Buffer
			err := serveMCPWithRun(strings.NewReader(in), &out, func(string) (string, error) {
				t.Fatal("blank prompt launched an engine")
				return "", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Error *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error == nil || response.Error.Code != -32602 {
				t.Fatalf("expected invalid params for %q, got %s", prompt, out.String())
			}
		})
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
