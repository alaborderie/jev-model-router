package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/alaborderie/jev-model-router/internal/router"
)

func TestPromptHookRecommendationAndSilentFailures(t *testing.T) {
	input := `{"prompt":"debug this"}`
	for _, tc := range []struct {
		name    string
		entry   router.Entry
		err     error
		wantOut bool
	}{
		{"ok", router.Entry{TargetID: "claude-opus", Effort: "high", Outcome: "ok"}, nil, true},
		{"fallback", router.Entry{TargetID: "claude-opus", Effort: "high", Outcome: "classifier_error"}, nil, false},
		{"timeout", router.Entry{}, errors.New("timeout"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := hookPromptWithRoute(strings.NewReader(input), &out, func(prompt string) (router.Entry, error) {
				if prompt != "debug this" {
					t.Fatalf("prompt changed: %q", prompt)
				}
				return tc.entry, tc.err
			})
			if err != nil {
				t.Fatal(err)
			}
			if (out.Len() > 0) != tc.wantOut {
				t.Fatalf("unexpected output: %q", out.String())
			}
		})
	}
}
