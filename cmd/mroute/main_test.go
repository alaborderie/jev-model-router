package main

import "testing"

func TestRoutePromptArguments(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		prompt  string
		json    bool
		wantErr bool
	}{
		{"literal json prompt", []string{"--", "--json"}, "--json", false, false},
		{"literal json prefix", []string{"--", "--json", "explain"}, "--json explain", false, false},
		{"json output", []string{"--json", "explain"}, "explain", true, false},
		{"json output with literal flag prompt", []string{"--json", "--", "--json"}, "--json", true, false},
		{"ordinary prompt", []string{"explain", "this"}, "explain this", false, false},
		{"missing prompt after separator", []string{"--"}, "", false, true},
		{"missing prompt after json separator", []string{"--json", "--"}, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt, jsonRoute, err := routePromptArgs(tc.args)
			if (err != nil) != tc.wantErr || (!tc.wantErr && (prompt != tc.prompt || jsonRoute != tc.json)) {
				t.Fatalf("routePromptArgs(%q) = %q, %v, %v; want %q, %v, error %v", tc.args, prompt, jsonRoute, err, tc.prompt, tc.json, tc.wantErr)
			}
		})
	}
}
