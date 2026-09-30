package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicServerDoesNotExposeRouting(t *testing.T) {
	server := httptest.NewServer(healthHandler())
	defer server.Close()

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/healthz", http.StatusOK},
		{"/", http.StatusOK},
		{"/styles.css", http.StatusOK},
		{"/route", http.StatusNotFound},
		{"/unknown", http.StatusNotFound},
	} {
		resp, err := server.Client().Get(server.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("GET %s: got %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}

func TestLandingAssets(t *testing.T) {
	for _, tc := range []struct {
		path, contentType, content string
	}{
		{"/", "text/html; charset=utf-8", "go install github.com/alaborderie/f-0923-jev-model-router/cmd/mroute@latest"},
		{"/styles.css", "text/css; charset=utf-8", "@media"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			healthHandler().ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
			if r.Code != http.StatusOK || r.Header().Get("Content-Type") != tc.contentType || !strings.Contains(r.Body.String(), tc.content) {
				t.Fatalf("GET %s: status=%d type=%q body=%q", tc.path, r.Code, r.Header().Get("Content-Type"), r.Body.String())
			}
		})
	}
}

func TestLandingDisclosesPrivateAccess(t *testing.T) {
	r := httptest.NewRecorder()
	healthHandler().ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	for _, notice := range []string{"Private preview", "Repository access required", "Private docs", "Private GitHub", "Private issues", "Anonymous visitors will see a GitHub 404"} {
		if !strings.Contains(r.Body.String(), notice) {
			t.Errorf("landing missing access notice %q", notice)
		}
	}
}

func TestPublicServerHeadAndMethods(t *testing.T) {
	server := httptest.NewServer(healthHandler())
	defer server.Close()
	for _, path := range []string{"/", "/styles.css", "/healthz"} {
		resp, err := server.Client().Head(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(body) != 0 {
			t.Fatalf("HEAD %s: status=%d body=%q err=%v", path, resp.StatusCode, body, err)
		}
		r := httptest.NewRecorder()
		healthHandler().ServeHTTP(r, httptest.NewRequest("POST", path, nil))
		if r.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s: status=%d", path, r.Code)
		}
	}
}

func TestServeRejectsArgumentsBeforeLoadingConfig(t *testing.T) {
	err := run([]string{"serve", "extra"})
	if err == nil || !strings.Contains(err.Error(), "usage: mroute serve") {
		t.Fatalf("run serve extra: got %v, want serve usage error", err)
	}
}
