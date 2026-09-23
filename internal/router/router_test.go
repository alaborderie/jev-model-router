package router

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/classifier"
	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

func testConfig() config.Config {
	return config.Config{Classifier: config.Classifier{Backend: "jev", ConfidenceGate: 0.6, TimeoutMS: 10, PreviewBytes: 100, Jev: config.Jev{Model: "jev-latest"}}, Routing: config.Routing{SafeTarget: "safe", DefaultEngine: "claude"}, Targets: []config.Target{{ID: "cheap", Engine: "claude", Model: "haiku", Efforts: []string{"low"}, When: "easy"}, {ID: "safe", Engine: "codex", Model: "strong", Efforts: []string{"high"}, When: "hard"}}}
}
func TestAdaptersKeepPromptAsArgument(t *testing.T) {
	prompt := "hello; touch /tmp/should-never-exist"
	for _, tc := range []struct {
		target config.Target
		want   string
	}{{config.Target{Engine: "claude", Model: "m"}, "-p --model m --effort high"}, {config.Target{Engine: "opencode", Model: "p/m"}, "run -m p/m --variant high"}, {config.Target{Engine: "codex", Model: "m"}, "exec -m m -c model_reasoning_effort=high"}} {
		_, args, err := Args(tc.target, "high", prompt)
		if err != nil {
			t.Fatal(err)
		}
		if args[len(args)-1] != prompt || strings.Join(args[:len(args)-1], " ") != tc.want {
			t.Fatalf("wrong argv: %q", args)
		}
	}
}
func TestFailureStillLaunchesSafeTarget(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "unit-test-key")
	for _, tc := range []struct {
		name   string
		status int
		sleep  time.Duration
		want   string
	}{{"401", 401, 0, "classifier_error"}, {"429", 429, 0, "classifier_error"}, {"timeout", 200, 30 * time.Millisecond, "timeout"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(tc.sleep); w.WriteHeader(tc.status) }))
			defer server.Close()
			c := testConfig()
			script := filepath.Join(t.TempDir(), "fake-engine")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			logPath := filepath.Join(t.TempDir(), "routes.jsonl")
			r := Runner{Config: c, Classifier: classifier.Jev{URL: server.URL}, LogPath: logPath, Stdout: &out, Stderr: &errOut, Command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				if name != "codex" {
					t.Errorf("launched %s", name)
				}
				return exec.CommandContext(ctx, script, args...)
			}}
			prompt := "fix this; echo secret"
			entry, err := r.Run(context.Background(), prompt)
			if err != nil {
				t.Fatal(err)
			}
			if entry.TargetID != "safe" || entry.Outcome != tc.want || !strings.Contains(out.String(), prompt) {
				t.Fatalf("bad route %+v output %q", entry, out.String())
			}
			if !strings.Contains(errOut.String(), "routed to safe") {
				t.Fatalf("missing stderr: %q", errOut.String())
			}
			lines, err := Read(logPath)
			if err != nil || len(lines) != 1 || lines[0].Outcome != tc.want {
				t.Fatalf("bad log %v %+v", err, lines)
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), prompt) || strings.Contains(string(raw), "unit-test-key") {
				t.Fatal("log leaked prompt or key")
			}
		})
	}
}

type staticClassifier struct {
	d   classifier.Decision
	err error
}

func (s staticClassifier) Classify(context.Context, string, config.Config) (classifier.Decision, error) {
	return s.d, s.err
}
func TestConfidenceGateAndClamping(t *testing.T) {
	c := testConfig()
	r := Runner{Config: c, Classifier: staticClassifier{d: classifier.Decision{TargetID: "cheap", Effort: "low", ChoiceConfidence: 0.2, ScoreConfidence: 0.9}}}
	e, err := r.Decide(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if e.TargetID != "safe" || e.Effort != "high" || e.Outcome != "low_confidence" {
		t.Fatalf("bad gate %+v", e)
	}
	r.Classifier = staticClassifier{err: &classifier.Error{Kind: classifier.Authentication, Err: errors.New("missing key")}}
	e, err = r.Decide(context.Background(), "task")
	if err != nil || e.Outcome != "classifier_error" {
		t.Fatalf("bad fallback %+v %v", e, err)
	}
}
