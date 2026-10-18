package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
)

func TestAgentHookFixtures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fixture   string
		wantModel string
		wantCalls int
	}{
		{"supported", "testdata/agent_supported.json", "haiku", 1},
		{"unsupported", "testdata/agent_unsupported.json", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := os.ReadFile(tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			calls := 0
			err = hookAgentWithRoute(bytes.NewReader(input), &out, func(prompt string) (router.Entry, error) {
				calls++
				if prompt != "Find the failing test" {
					t.Fatalf("prompt changed: %q", prompt)
				}
				return router.Entry{Engine: "claude", Model: "claude-haiku-4-5", Outcome: "ok"}, nil
			})
			if err != nil || calls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if tc.wantModel == "" {
				if out.Len() != 0 {
					t.Fatalf("unsupported event emitted %q", out.String())
				}
				return
			}
			var result struct {
				HookSpecificOutput struct {
					UpdatedInput map[string]any `json:"updatedInput"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			updated := result.HookSpecificOutput.UpdatedInput
			if updated["model"] != tc.wantModel || updated["description"] != "Inspect tests" || updated["prompt"] != "Find the failing test" {
				t.Fatalf("wrong updated input: %v", updated)
			}
		})
	}
}

func TestAgentHookSkipsOtherEnginesAndExplicitModels(t *testing.T) {
	for _, tc := range []struct {
		input string
		entry router.Entry
	}{
		{`{"tool_name":"Agent","tool_input":{"prompt":"test"}}`, router.Entry{Engine: "codex", Model: "gpt", Outcome: "ok"}},
		{`{"tool_name":"Agent","tool_input":{"prompt":"test","model":"sonnet"}}`, router.Entry{Engine: "claude", Model: "claude-haiku-4-5", Outcome: "ok"}},
	} {
		var out bytes.Buffer
		if err := hookAgentWithRoute(strings.NewReader(tc.input), &out, func(string) (router.Entry, error) { return tc.entry, nil }); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("unexpected output %q", out.String())
		}
	}
}
