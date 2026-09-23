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

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	c := config.Config{Classifier: config.Classifier{Backend: "jev", ConfidenceGate: 0.6, TimeoutMS: 2000, PreviewBytes: 8, Jev: config.Jev{Model: "jev-latest"}}, Routing: config.Routing{SafeTarget: "big", DefaultEngine: "claude"}, Targets: []config.Target{{ID: "small", Engine: "claude", Model: "haiku", Efforts: []string{"low", "medium"}, When: "easy tasks"}, {ID: "big", Engine: "claude", Model: "opus", Efforts: []string{"medium", "high"}, When: "hard tasks"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestJevRequestAndResponse(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "secret-test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer secret-test-key" {
			t.Errorf("bad request %s", r.URL)
		}
		var body struct {
			State     string `json:"state"`
			Model     string `json:"model"`
			Questions map[string]struct {
				Type     string          `json:"type"`
				Criteria json.RawMessage `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.State != "12345678" || body.Model != "jev-latest" || len(body.Questions) != 2 || body.Questions["target"].Type != "choice" || body.Questions["effort"].Type != "score" {
			t.Errorf("bad payload %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"target":{"type":"choice","choice":"big","confidence":0.87},"effort":{"type":"score","score":1.2,"confidence":0.72}}}`))
	}))
	defer server.Close()
	d, err := (Jev{URL: server.URL + "/v1/systemone"}).Classify(context.Background(), "123456789abcdef", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if d.TargetID != "big" || d.Effort != "medium" || d.ChoiceConfidence != 0.87 || d.ScoreConfidence != 0.72 || d.Latency <= 0 {
		t.Fatalf("bad decision %+v", d)
	}
}
func TestJevFailures(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "secret-test-key")
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   ErrorKind
	}{{"auth", 401, "{}", Authentication}, {"validation", 422, "{}", Validation}, {"rate", 429, "{}", RateLimit}, {"overload", 529, "{}", Overloaded}, {"malformed", 200, "broken", InvalidResponse}, {"unknown target", 200, `{"answers":{"target":{"type":"choice","choice":"unknown","confidence":0.9},"effort":{"type":"score","score":1,"confidence":0.9}}}`, InvalidResponse}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			_, err := (Jev{URL: s.URL}).Classify(context.Background(), "prompt", testConfig(t))
			var ce *Error
			if !errors.As(err, &ce) || ce.Kind != tc.kind || strings.Contains(err.Error(), "secret-test-key") {
				t.Fatalf("bad error %v", err)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(30 * time.Millisecond) }))
		defer s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		_, err := (Jev{URL: s.URL}).Classify(ctx, "prompt", testConfig(t))
		var ce *Error
		if !errors.As(err, &ce) || ce.Kind != Timeout {
			t.Fatalf("bad timeout %v", err)
		}
	})
}
