package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthServerOnlyExposesReadiness(t *testing.T) {
	server := httptest.NewServer(healthHandler())
	defer server.Close()

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/healthz", http.StatusOK},
		{"/", http.StatusNotFound},
		{"/route", http.StatusNotFound},
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

func TestServeRejectsArgumentsBeforeLoadingConfig(t *testing.T) {
	err := run([]string{"serve", "extra"})
	if err == nil || !strings.Contains(err.Error(), "usage: mroute serve") {
		t.Fatalf("run serve extra: got %v, want serve usage error", err)
	}
}
