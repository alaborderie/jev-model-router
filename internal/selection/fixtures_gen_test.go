package selection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regenerate with: MROUTE_WRITE_SELECTION_FIXTURES=1 go test ./internal/selection -run TestWriteFixtures
const writeFixturesEnv = "MROUTE_WRITE_SELECTION_FIXTURES"

const singletonTemplate = `{
  "schema_version": 1,
  "request_id": "req-singleton-0001",
  "candidate_set_hash": "@HASH@",
  "role_policy_version": "developer-v1",
  "role_brief": {
    "objective": "Implement and test the bounded change",
    "constraints": [
      "no network access",
      "tests first"
    ]
  },
  "state": {
    "role": "developer",
    "task_kind": "implementation",
    "quota_remaining_pct": "42.5",
    "urgent": false,
    "blocked_by": null
  },
  "candidates": [
    {
      "candidate_id": "opencode/test-provider/model-a:high",
      "access_id": "access.test-01",
      "engine": "opencode",
      "provider_id": "test-provider",
      "model_id": "model-a",
      "transport": "cli",
      "variant": "high",
      "capability_profile_version": "caps-2026.10",
      "configuration_revision": 3,
      "description": "Small tested code change; weekly quota 58% remaining"
    }
  ]
}
`

const emptyTemplate = `{
  "schema_version": 1,
  "request_id": "req-empty-0001",
  "candidate_set_hash": "@HASH@",
  "role_policy_version": "reviewer-v1",
  "role_brief": {
    "objective": "Review the pending change"
  },
  "state": {
    "role": "reviewer",
    "notes": []
  },
  "candidates": []
}
`

const unicodeTemplate = `{
  "schema_version": 1,
  "request_id": "req-unicode-0001",
  "candidate_set_hash": "@HASH@",
  "role_policy_version": "developer-v2",
  "role_brief": {
    "objective": "Choisir le modèle — 選択 <b> & </b>",
    "notes": [
      "tab\there",
      "line@LS@sep@PS@para",
      "escaped \u2028 and \u2029"
    ]
  },
  "state": {
    "venture": "Ölçü café 🧪 日本語",
    "quote": "say \"hi\" \\ bye",
    "flags": {
      "urgent": true,
      "blocked_by": null
    },
    "controls": [
      "\u0001",
      "x\u007Fy",
      "literal \\u2028 text",
      "real@LS@char"
    ]
  },
  "candidates": [
    {
      "candidate_id": "cand-c",
      "access_id": "access.test-03",
      "engine": "claude",
      "provider_id": "test-anthropic",
      "model_id": "model-c",
      "transport": "cli",
      "variant": "max",
      "capability_profile_version": "caps-2026.10",
      "configuration_revision": 12,
      "description": "Café naïve résumé — 日本語のテキスト 🚀 <b>&amp;</b> \"quoted\" back\\slash@LS@line\nbreak\ttab"
    },
    {
      "candidate_id": "cand-a",
      "access_id": "access.test-01",
      "engine": "opencode",
      "provider_id": "test-provider",
      "model_id": "model-a",
      "transport": "cli",
      "variant": null,
      "capability_profile_version": "caps-2026.10",
      "configuration_revision": 1,
      "description": "ctrl \u0001 del \u007F ls-text \\u2028 real-ps \u2029 pair \ud83d\ude80 end"
    },
    {
      "candidate_id": "cand-b",
      "access_id": "access.test-02",
      "engine": "codex",
      "provider_id": "test-openai",
      "model_id": "model-b",
      "transport": "cli",
      "variant": "high",
      "capability_profile_version": "caps-2026.09",
      "configuration_revision": 9007199254740991,
      "description": "中文 한국어 Ünïcödé@PS@quota \"unknown\"; ratio 3 < 4 & 5 > 2 \\ end"
    }
  ]
}
`

func pretty(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func fill(t *testing.T, template string) []byte {
	t.Helper()
	text := strings.NewReplacer("@LS@", "\u2028", "@PS@", "\u2029").Replace(template)
	v, err := parseDocument([]byte(strings.Replace(text, "@HASH@", strings.Repeat("0", 64), 1)))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.Replace(text, "@HASH@", rawHash(t, v.(map[string]any)["candidates"].([]any)), 1))
}
func generic(t *testing.T, b []byte) map[string]any {
	t.Helper()
	v, err := parseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}
func mustReplace(t *testing.T, b []byte, old, new string) []byte {
	t.Helper()
	out := bytes.Replace(b, []byte(old), []byte(new), 1)
	if bytes.Equal(out, b) {
		t.Fatalf("replacement %q did not apply", old)
	}
	return out
}

func TestWriteFixtures(t *testing.T) {
	if os.Getenv(writeFixturesEnv) != "1" {
		t.Skip("set " + writeFixturesEnv + "=1 to regenerate fixtures")
	}
	files := map[string][]byte{}
	var manifest []manifestEntry
	addRequest := func(name string, b []byte, valid bool) {
		files[name] = b
		e := manifestEntry{File: name, Kind: "request", Valid: valid}
		if valid {
			req, err := Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			e.CandidateSetHash = &req.CandidateSetHash
		}
		manifest = append(manifest, e)
	}
	singleton, empty, unicode := fill(t, singletonTemplate), fill(t, emptyTemplate), fill(t, unicodeTemplate)
	addRequest("empty.json", empty, true)
	addRequest("singleton.json", singleton, true)
	addRequest("unicode.json", unicode, true)

	hash := string(generic(t, singleton)["candidate_set_hash"].(string))
	addRequest("invalid/trailing-value.json", append(append([]byte{}, singleton...), "{}\n"...), false)
	addRequest("invalid/duplicate-key-top.json", mustReplace(t, singleton, `  "role_policy_version": "developer-v1",`, "  \"role_policy_version\": \"developer-v1\",\n  \"role_policy_version\": \"developer-v1\","), false)
	addRequest("invalid/duplicate-key-nested-state.json", mustReplace(t, singleton, `    "role": "developer",`, "    \"role\": \"developer\",\n    \"role\": \"reviewer\","), false)
	addRequest("invalid/hash-mismatch.json", mustReplace(t, singleton, hash, rawHash(t, nil)), false)
	addRequest("invalid/unknown-field.json", mustReplace(t, singleton, `  "role_policy_version": "developer-v1",`, "  \"role_policy_version\": \"developer-v1\",\n  \"priority\": \"high\","), false)
	addRequest("invalid/float-in-state.json", mustReplace(t, singleton, `"quota_remaining_pct": "42.5"`, `"quota_remaining_pct": 42.5`), false)
	addRequest("invalid/lone-surrogate.json", mustReplace(t, singleton, `"task_kind": "implementation"`, `"task_kind": "implementation \ud83d"`), false)
	addRequest("invalid/bad-id-char.json", mustReplace(t, singleton, `"role_policy_version": "developer-v1"`, `"role_policy_version": "developer v1"`), false)
	addRequest("invalid/schema-version-2.json", mustReplace(t, singleton, `"schema_version": 1`, `"schema_version": 2`), false)

	m := generic(t, singleton)
	c := m["candidates"].([]any)[0].(map[string]any)
	m["candidates"] = []any{c, c}
	m["candidate_set_hash"] = rawHash(t, m["candidates"].([]any))
	addRequest("invalid/duplicate-candidate-id.json", pretty(t, m), false)

	m = generic(t, singleton)
	m["candidates"].([]any)[0].(map[string]any)["configuration_revision"] = json.Number("0")
	m["candidate_set_hash"] = rawHash(t, m["candidates"].([]any))
	addRequest("invalid/revision-zero.json", pretty(t, m), false)

	m = generic(t, singleton)
	var many []any
	for i := 0; i < 33; i++ {
		x := testCandidate(i)
		x["description"] = fmt.Sprintf("Synthetic candidate %02d; daily quota unknown", i)
		many = append(many, x)
	}
	m["candidates"] = many
	m["candidate_set_hash"] = rawHash(t, many)
	addRequest("invalid/too-many-candidates.json", pretty(t, m), false)

	m = generic(t, singleton)
	var big []any
	for i := 0; i < 32; i++ {
		id := func(kind string) string {
			return fmt.Sprintf("%s-%02d-", kind, i) + strings.Repeat("x", MaxIDBytes-len(kind)-4)
		}
		big = append(big, map[string]any{"candidate_id": id("cand"), "access_id": id("access"), "engine": id("engine"), "provider_id": id("provider"), "model_id": id("model"), "transport": id("transport"), "variant": id("variant"), "capability_profile_version": id("caps"), "configuration_revision": json.Number("1"), "description": fmt.Sprintf("Candidate %02d: ", i) + strings.Repeat("quota-fact ", 91)})
	}
	m["candidates"] = big
	m["candidate_set_hash"] = rawHash(t, big)
	over := pretty(t, m)
	if len(over) <= MaxRequestBytes {
		t.Fatalf("oversize fixture only %d bytes", len(over))
	}
	if _, err := decodeBytes(over); err != nil {
		t.Fatalf("oversize fixture has another defect: %v", err)
	}
	addRequest("invalid/oversize.json", over, false)

	for name, b := range files {
		if !strings.HasPrefix(name, "invalid/") {
			continue
		}
		if _, err := Decode(bytes.NewReader(b)); err == nil || err.Error() != "invalid_request: "+invalidFixtureCodes[name] {
			t.Fatalf("%s: %v", name, err)
		}
	}

	reqs := map[string]Request{}
	for _, name := range []string{"empty.json", "singleton.json", "unicode.json"} {
		r, err := Decode(bytes.NewReader(files[name]))
		if err != nil {
			t.Fatal(err)
		}
		reqs[name] = r
	}
	base := func(name string) Response {
		r := reqs[name]
		return Response{SchemaVersion: SchemaVersion, RequestID: r.RequestID, CandidateSetHash: r.CandidateSetHash, LatencyMS: 0}
	}
	model, usage := ptr("jev-1.13.0"), &Usage{InputTokens: 812, OutputTokens: 3}
	single, uni := ptr(reqs["singleton.json"].Candidates[0].CandidateID), ptr("cand-b")
	type resp struct {
		file, req string
		valid     bool
		r         Response
	}
	mod := func(r Response, f func(*Response)) Response { f(&r); return r }
	selected := mod(base("singleton.json"), func(r *Response) { r.Status, r.Reason, r.CandidateID = StatusSelected, ReasonSingleton, single })
	classified := mod(base("unicode.json"), func(r *Response) {
		r.Status, r.Reason, r.CandidateID, r.Confidence, r.SelectorModel, r.LatencyMS, r.Usage = StatusSelected, ReasonClassified, uni, ptr(0.83), model, 312, usage
	})
	responses := []resp{
		{"singleton-selected.json", "singleton.json", true, selected},
		{"empty-abstained.json", "empty.json", true, mod(base("empty.json"), func(r *Response) { r.Status, r.Reason = StatusAbstained, ReasonNoCandidates })},
		{"unicode-classified.json", "unicode.json", true, classified},
		{"unicode-low-confidence.json", "unicode.json", true, mod(base("unicode.json"), func(r *Response) {
			r.Status, r.Reason, r.Confidence, r.SelectorModel, r.LatencyMS, r.Usage = StatusAbstained, ReasonLowConfidence, ptr(0.41), model, 287, usage
		})},
		{"unicode-error-timeout.json", "unicode.json", true, mod(base("unicode.json"), func(r *Response) { r.Status, r.Reason, r.LatencyMS = StatusError, ReasonTimeout, 2001 })},
		{"invalid/stale-request-id.json", "unicode.json", false, mod(classified, func(r *Response) { r.RequestID = "req-unicode-0000" })},
		{"invalid/stale-hash.json", "unicode.json", false, mod(classified, func(r *Response) { r.CandidateSetHash = reqs["singleton.json"].CandidateSetHash })},
		{"invalid/out-of-set-candidate.json", "unicode.json", false, mod(classified, func(r *Response) { r.CandidateID = ptr("cand-z") })},
		{"invalid/selected-null-id.json", "unicode.json", false, mod(classified, func(r *Response) { r.CandidateID = nil })},
		{"invalid/abstained-with-id.json", "empty.json", false, mod(base("empty.json"), func(r *Response) {
			r.Status, r.Reason, r.CandidateID = StatusAbstained, ReasonNoCandidates, ptr("cand-a")
		})},
		{"invalid/confidence-above-one.json", "unicode.json", false, mod(classified, func(r *Response) { r.Confidence = ptr(1.5) })},
		{"invalid/singleton-reason-multi.json", "unicode.json", false, mod(base("unicode.json"), func(r *Response) { r.Status, r.Reason, r.CandidateID = StatusSelected, ReasonSingleton, uni })},
	}
	for _, x := range responses {
		name := "responses/" + x.file
		err := ValidateResponse(reqs[x.req], x.r)
		if (err == nil) != x.valid {
			t.Fatalf("%s: validity %v, err %v", name, x.valid, err)
		}
		files[name] = pretty(t, x.r)
		manifest = append(manifest, manifestEntry{File: name, Kind: "response", Valid: x.valid, Request: ptr(x.req)})
	}
	files["manifest.json"] = pretty(t, manifest)
	for name, b := range files {
		path := filepath.Join(fixtureDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
