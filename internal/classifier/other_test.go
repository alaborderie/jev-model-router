package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHeuristicsOffline(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	c := testConfig(t)
	for _, prompt := range []string{"simple easy tasks", "debug hard tasks", "unmatched words"} {
		d, err := (Heuristics{}).Classify(context.Background(), prompt, c)
		if err != nil {
			t.Fatal(err)
		}
		target, ok := c.Target(d.TargetID)
		if !ok {
			t.Fatalf("unknown target %q", d.TargetID)
		}
		valid := false
		for _, e := range target.Efforts {
			if e == d.Effort {
				valid = true
			}
		}
		if !valid {
			t.Fatalf("unsupported effort %+v", d)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Heuristics{}).Classify(ctx, "x", c); err == nil {
		t.Fatal("expected cancellation")
	}
}
func TestOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "" {
			t.Errorf("bad request %s", r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Errorf("missing schema: %v", format)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"target_id\":\"small\",\"effort\":\"low\"}"}}]}`))
	}))
	defer s.Close()
	c := testConfig(t)
	c.Classifier.Backend = "openai"
	c.Classifier.OpenAI.BaseURL = s.URL + "/v1"
	c.Classifier.OpenAI.Model = "local"
	d, err := (OpenAI{}).Classify(context.Background(), "simple task", c)
	if err != nil {
		t.Fatal(err)
	}
	if d.TargetID != "small" || d.Effort != "low" {
		t.Fatalf("bad decision %+v", d)
	}
}
func TestOpenAIFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		kind ErrorKind
	}{{"rate", 429, "{}", RateLimit}, {"malformed", 200, "{}", InvalidResponse}, {"unknown target", 200, `{"choices":[{"message":{"content":"{\"target_id\":\"other\",\"effort\":\"low\"}"}}]}`, InvalidResponse}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) }))
			defer s.Close()
			c := testConfig(t)
			c.Classifier.OpenAI.BaseURL = s.URL + "/v1"
			_, err := (OpenAI{}).Classify(context.Background(), "x", c)
			var ce *Error
			if !errors.As(err, &ce) || ce.Kind != tc.kind {
				t.Fatalf("bad error %v", err)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(30 * time.Millisecond) }))
		defer s.Close()
		c := testConfig(t)
		c.Classifier.OpenAI.BaseURL = s.URL + "/v1"
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		_, err := (OpenAI{}).Classify(ctx, "x", c)
		var ce *Error
		if !errors.As(err, &ce) || ce.Kind != Timeout {
			t.Fatalf("bad timeout %v", err)
		}
	})
	t.Run("network", func(t *testing.T) {
		c := testConfig(t)
		c.Classifier.OpenAI.BaseURL = "http://127.0.0.1:1/v1"
		_, err := (OpenAI{}).Classify(context.Background(), "x", c)
		var ce *Error
		if !errors.As(err, &ce) || ce.Kind != Network || strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatalf("bad network error %v", err)
		}
	})
}
