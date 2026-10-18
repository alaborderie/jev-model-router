package selection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixtureDir = "../../testdata/selection-v1"

// requirePythonEnv turns a missing python3 into a failure instead of a skip.
const requirePythonEnv = "MROUTE_REQUIRE_PYTHON"

type manifestEntry struct {
	File             string  `json:"file"`
	Kind             string  `json:"kind"`
	Valid            bool    `json:"valid"`
	Request          *string `json:"request"`
	CandidateSetHash *string `json:"candidate_set_hash"`
	Error            *string `json:"error"`
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func readManifest(t *testing.T) []manifestEntry {
	t.Helper()
	var m []manifestEntry
	if err := json.Unmarshal(readFixture(t, "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func decodeFixture(t *testing.T, name string) Request {
	t.Helper()
	req, err := Decode(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return req
}
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidRequest) || err.Error() != "invalid_request: "+code {
		t.Fatalf("want invalid_request: %s, got %v", code, err)
	}
}
func wantResponseCode(t *testing.T, err error, code string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidResponse) || err.Error() != "invalid_response: "+code {
		t.Fatalf("want invalid_response: %s, got %v", code, err)
	}
}

func testCandidate(i int) map[string]any {
	return map[string]any{"candidate_id": "cand-" + string(rune('a'+i/26)) + string(rune('a'+i%26)), "access_id": "access-1", "engine": "opencode", "provider_id": "test", "model_id": "model-a", "transport": "cli", "variant": "high", "capability_profile_version": "caps-v1", "configuration_revision": json.Number("1"), "description": "Synthetic candidate; quota unknown"}
}
func testRequest(n int) map[string]any {
	c := []any{}
	for i := 0; i < n; i++ {
		c = append(c, testCandidate(i))
	}
	return map[string]any{"schema_version": json.Number("1"), "request_id": "req-1", "candidate_set_hash": "", "role_policy_version": "developer-v1", "role_brief": map[string]any{"objective": "test"}, "state": map[string]any{"role": "developer"}, "candidates": c}
}
func rawHash(t *testing.T, cands []any) string {
	t.Helper()
	s := append([]any(nil), cands...)
	id := func(v any) string {
		m, _ := v.(map[string]any)
		s, _ := m["candidate_id"].(string)
		return s
	}
	slices.SortStableFunc(s, func(a, b any) int { return strings.Compare(id(a), id(b)) })
	b, err := canonical(s)
	if err != nil {
		return strings.Repeat("0", 64)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func encodeRequest(t *testing.T, m map[string]any) []byte {
	t.Helper()
	if m["candidate_set_hash"] == "" {
		cands, _ := m["candidates"].([]any)
		m["candidate_set_hash"] = rawHash(t, cands)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func decodeMap(t *testing.T, m map[string]any) (Request, error) {
	t.Helper()
	return Decode(bytes.NewReader(encodeRequest(t, m)))
}
func nested(levels int, leaf any, array bool) any {
	v := leaf
	for i := 0; i < levels; i++ {
		if array {
			v = []any{v}
		} else {
			v = map[string]any{"a": v}
		}
	}
	return v
}

func TestManifestFixtures(t *testing.T) {
	entries := readManifest(t)
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(readFixture(t, "manifest.json"), &raw); err != nil || len(raw) != len(entries) {
		t.Fatalf("manifest not a flat entry list: %v", err)
	}
	for i, r := range raw {
		if len(r) != 6 {
			t.Errorf("%s: entry must carry exactly file, kind, valid, request, candidate_set_hash, error", entries[i].File)
		}
		if _, ok := r["error"]; !ok {
			t.Errorf("%s: entry lacks the error key", entries[i].File)
		}
	}
	seen := map[string]bool{}
	invalidRequests, invalidResponses := 0, 0
	for _, e := range entries {
		if seen[e.File] {
			t.Errorf("duplicate manifest entry %s", e.File)
		}
		seen[e.File] = true
		t.Run(e.File, func(t *testing.T) {
			if e.Valid != (e.Error == nil) {
				t.Fatalf("valid=%v but error=%v", e.Valid, e.Error)
			}
			if e.Error != nil && (*e.Error == "" || strings.Contains(*e.Error, ":")) {
				t.Fatalf("error must be the bare code, got %q", *e.Error)
			}
			switch e.Kind {
			case "request":
				req, err := Decode(bytes.NewReader(readFixture(t, e.File)))
				if !e.Valid {
					invalidRequests++
					if e.CandidateSetHash != nil || e.Request != nil {
						t.Fatal("invalid request entry carries hash or request")
					}
					wantCode(t, err, *e.Error)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				h, err := CandidateHash(req.Candidates)
				if err != nil || e.CandidateSetHash == nil || h != *e.CandidateSetHash || req.CandidateSetHash != h {
					t.Fatalf("hash mismatch: %s %v", h, err)
				}
			case "response":
				if e.Request == nil || e.CandidateSetHash != nil {
					t.Fatal("response entry must name its request only")
				}
				req := decodeFixture(t, *e.Request)
				dec := json.NewDecoder(bytes.NewReader(readFixture(t, e.File)))
				dec.DisallowUnknownFields()
				var resp Response
				if err := dec.Decode(&resp); err != nil {
					t.Fatal(err)
				}
				err := ValidateResponse(req, resp)
				if e.Valid {
					if err != nil {
						t.Fatalf("valid response rejected: %v", err)
					}
					return
				}
				invalidResponses++
				wantResponseCode(t, err, *e.Error)
			default:
				t.Fatalf("unknown kind %q", e.Kind)
			}
		})
	}
	if invalidRequests == 0 || invalidResponses == 0 {
		t.Errorf("manifest lacks invalid fixtures: %d requests, %d responses", invalidRequests, invalidResponses)
	}
	for _, name := range []string{"empty.json", "singleton.json", "unicode.json"} {
		if !seen[name] {
			t.Errorf("manifest misses %s", name)
		}
	}
	var onDisk []string
	for _, pattern := range []string{"*.json", "*/*.json", "*/*/*.json"} {
		files, err := filepath.Glob(filepath.Join(fixtureDir, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		onDisk = append(onDisk, files...)
	}
	for _, f := range onDisk {
		rel, _ := filepath.Rel(fixtureDir, f)
		rel = filepath.ToSlash(rel)
		if rel != "manifest.json" && !seen[rel] {
			t.Errorf("fixture %s not in manifest", rel)
		}
		delete(seen, rel)
	}
	for name := range seen {
		t.Errorf("manifest names missing file %s", name)
	}
}

func TestEscapedDuplicateKeyFixture(t *testing.T) {
	b := readFixture(t, "invalid/duplicate-key-escaped.json")
	if !bytes.Contains(b, []byte(`"`+"\\"+`u0072ole"`)) {
		t.Fatal("fixture does not contain an escaped key on disk")
	}
}

func TestDecodeValidFixtures(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	if len(req.Candidates) != 3 || req.Candidates[0].CandidateID != "cand-c" || req.Candidates[1].Variant != nil || req.Candidates[2].Variant == nil {
		t.Fatalf("candidates not preserved in input order: %+v", req.Candidates)
	}
	if req.SchemaVersion != 1 || req.RequestID == "" || req.RolePolicyVersion == "" {
		t.Fatalf("bad request fields %+v", req)
	}
	for _, raw := range []json.RawMessage{req.RoleBrief, req.State} {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var g any
		if err := d.Decode(&g); err != nil {
			t.Fatal(err)
		}
		c, err := canonical(g)
		if err != nil || !bytes.Equal(c, raw) {
			t.Fatalf("role_brief/state not canonical: %s", raw)
		}
	}
	if !bytes.Contains(req.State, []byte("\u2028")) || !bytes.Contains(req.State, []byte(`\u0001`)) {
		t.Fatalf("state lost characters: %s", req.State)
	}
	empty := decodeFixture(t, "empty.json")
	if empty.Candidates == nil || len(empty.Candidates) != 0 {
		t.Fatal("empty candidates should decode to an empty slice")
	}
	if s := decodeFixture(t, "singleton.json"); len(s.Candidates) != 1 || s.Candidates[0].Variant == nil || s.Candidates[0].ConfigurationRevision != 3 {
		t.Fatalf("singleton: %+v", s.Candidates)
	}
}

func TestDecodeRejectsTrailingValue(t *testing.T) {
	b, err := os.ReadFile("../../testdata/selection-v1/singleton.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(bytes.NewReader(append(b, []byte(" {}")...)))
	if err == nil {
		t.Fatal("accepted a second JSON value")
	}
	wantCode(t, err, "trailing_data")
	for _, tail := range []string{"]", "}", "{}", "x", "null", "1", `""`, "\x00"} {
		_, err = Decode(bytes.NewReader(append(append([]byte{}, b...), tail...)))
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("accepted trailing %q", tail)
		}
	}
	for _, ws := range []string{"", "\n", " \t\r\n  "} {
		if _, err := Decode(bytes.NewReader(append([]byte(ws), append(append([]byte{}, b...), ws...)...))); err != nil {
			t.Fatalf("rejected whitespace %q: %v", ws, err)
		}
	}
}

func TestDecodeRejectsMalformedDocuments(t *testing.T) {
	for _, tc := range []struct{ in, code string }{{"", "syntax"}, {"   ", "syntax"}, {"[]", "not_object"}, {`"x"`, "not_object"}, {"null", "not_object"}, {"{", "syntax"}, {`{"a":}`, "syntax"}, {"{}", "missing_field"}} {
		_, err := Decode(strings.NewReader(tc.in))
		wantCode(t, err, tc.code)
	}
}

func TestDecodeRejectsDuplicateKeys(t *testing.T) {
	base := encodeRequest(t, testRequest(2))
	for name, edit := range map[string][2]string{
		"top":       {`"request_id":"req-1"`, `"request_id":"req-1","request_id":"req-1"`},
		"candidate": {`"access_id":"access-1"`, `"access_id":"access-1","access_id":"access-2"`},
		"state":     {`"state":{"role":"developer"}`, `"state":{"role":"developer","x":{"y":[{"k":"a","k":"b"}]}}`},
		"brief":     {`"role_brief":{"objective":"test"}`, `"role_brief":{"objective":"test","objective":"other"}`},
		"escaped":   {`"state":{"role":"developer"}`, `"state":{"role":"developer","\u0072ole":"x"}`},
		"non-bmp":   {`"state":{"role":"developer"}`, `"state":{"role":"developer","🚀":"a","\ud83d\ude80":"b"}`},
		"non-bmp-2": {`"state":{"role":"developer"}`, `"state":{"role":"developer","\uD83D\uDE80":"a","🚀":"b"}`},
	} {
		in := bytes.Replace(base, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(in, base) {
			t.Fatalf("%s: edit did not apply", name)
		}
		if strings.HasPrefix(name, "escaped") || strings.HasPrefix(name, "non-bmp") {
			if !strings.Contains(edit[1], `\u`) {
				t.Fatalf("%s: case lost its escape", name)
			}
		}
		_, err := Decode(bytes.NewReader(in))
		wantCode(t, err, "duplicate_key")
	}
}

func TestDecodeRejectsInvalidEncoding(t *testing.T) {
	base := encodeRequest(t, testRequest(1))
	if _, err := Decode(bytes.NewReader(base)); err != nil {
		t.Fatal(err)
	}
	_, err := Decode(bytes.NewReader(append([]byte("\xef\xbb\xbf"), base...)))
	wantCode(t, err, "bom")
	for _, bad := range []string{"\xff", "\xc3", "\xed\xa0\x80", "\xc0\xaf"} {
		in := bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":"dev`+bad+`eloper"`), 1)
		_, err := Decode(bytes.NewReader(in))
		wantCode(t, err, "invalid_utf8")
	}
}

func TestDecodeSurrogates(t *testing.T) {
	base := encodeRequest(t, testRequest(1))
	for _, tc := range []struct {
		text string
		ok   bool
	}{{`\ud83d\ude80`, true}, {`\uD83D\uDE80`, true}, {`\\ud800`, true}, {`\\\ud83d\ude80`, true}, {`\ud800`, false}, {`\udbff x`, false}, {`\udc00`, false}, {`\ud800\u0041`, false}, {`\ud800\ud800`, false}, {`\ude80\ud83d`, false}, {`\\\ud800`, false}} {
		in := bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":"`+tc.text+`"`), 1)
		_, err := Decode(bytes.NewReader(in))
		if tc.ok && err != nil {
			t.Fatalf("%s rejected: %v", tc.text, err)
		}
		if !tc.ok {
			wantCode(t, err, "surrogate")
		}
	}
	m := testRequest(1)
	m["candidates"].([]any)[0].(map[string]any)["description"] = "rocket 🚀"
	in := bytes.Replace(encodeRequest(t, m), []byte("🚀"), []byte(`\ud83d\ude80`), 1)
	req, err := Decode(bytes.NewReader(in))
	if err != nil || req.Candidates[0].Description != "rocket 🚀" {
		t.Fatalf("surrogate pair in description: %v", err)
	}
}

func TestDecodeTotalSizeBound(t *testing.T) {
	base := readFixture(t, "singleton.json")
	pad := func(n int) []byte {
		return append(append([]byte{}, base...), bytes.Repeat([]byte(" "), n-len(base))...)
	}
	if _, err := Decode(bytes.NewReader(pad(65536))); err != nil {
		t.Fatalf("65536 bytes rejected: %v", err)
	}
	_, err := Decode(bytes.NewReader(pad(65537)))
	wantCode(t, err, "too_large")
	_, err = Decode(bytes.NewReader(pad(1 << 20)))
	wantCode(t, err, "too_large")
}

func TestDecodeOversizeFixtureOnlyTooLarge(t *testing.T) {
	b := readFixture(t, "invalid/oversize.json")
	if len(b) <= 65536 {
		t.Fatalf("oversize fixture is %d bytes", len(b))
	}
	if _, err := decodeBytes(b); err != nil {
		t.Fatalf("oversize fixture invalid for another reason: %v", err)
	}
}

func TestDecodeStructuredBounds(t *testing.T) {
	for _, tc := range []struct {
		field     string
		unit      string
		limit, fx int
	}{{"role_brief", "é", 4096, 2}, {"state", "€", 12288, 3}, {"role_brief", "\x01", 4096, 6}} {
		content := tc.limit - len(`{"k":""}`)
		s := strings.Repeat(tc.unit, content/tc.fx) + strings.Repeat("a", content%tc.fx)
		m := testRequest(2)
		m[tc.field] = map[string]any{"k": s}
		req, err := decodeMap(t, m)
		if err != nil {
			t.Fatalf("%s at limit rejected: %v", tc.field, err)
		}
		raw := req.RoleBrief
		if tc.field == "state" {
			raw = req.State
		}
		if len(raw) != tc.limit {
			t.Fatalf("%s canonical size %d", tc.field, len(raw))
		}
		m[tc.field] = map[string]any{"k": s + "a"}
		m["candidate_set_hash"] = ""
		_, err = decodeMap(t, m)
		wantCode(t, err, tc.field+"_size")
	}
}

func TestDecodeDepthBound(t *testing.T) {
	for _, field := range []string{"role_brief", "state"} {
		for _, array := range []bool{false, true} {
			m := testRequest(1)
			if array {
				m[field] = map[string]any{"a": nested(31, "leaf", true)}
			} else {
				m[field] = nested(32, "leaf", false)
			}
			if _, err := decodeMap(t, m); err != nil {
				t.Fatalf("%s depth 32 rejected: %v", field, err)
			}
			if array {
				m[field] = map[string]any{"a": nested(32, "leaf", true)}
			} else {
				m[field] = nested(33, "leaf", false)
			}
			_, err := decodeMap(t, m)
			wantCode(t, err, "too_deep")
		}
	}
	_, err := Decode(strings.NewReader(strings.Repeat("[", 5000) + strings.Repeat("]", 5000)))
	wantCode(t, err, "too_deep")
}

func TestDecodeCandidateCountBound(t *testing.T) {
	req, err := decodeMap(t, testRequest(32))
	if err != nil || len(req.Candidates) != 32 {
		t.Fatalf("32 candidates rejected: %v", err)
	}
	_, err = decodeMap(t, testRequest(33))
	wantCode(t, err, "too_many_candidates")
	_, err = decodeMap(t, testRequest(0))
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeDescriptionBound(t *testing.T) {
	for _, tc := range []struct {
		desc string
		ok   bool
	}{{strings.Repeat("€", 341) + "a", true}, {strings.Repeat("€", 341) + "ab", false}, {strings.Repeat("é", 512), true}, {strings.Repeat("é", 512) + "a", false}, {"x", true}, {"", false}} {
		m := testRequest(2)
		m["candidates"].([]any)[1].(map[string]any)["description"] = tc.desc
		_, err := decodeMap(t, m)
		if tc.ok && err != nil {
			t.Fatalf("%d bytes rejected: %v", len(tc.desc), err)
		}
		if !tc.ok {
			wantCode(t, err, "description")
		}
	}
}

func TestDecodeIDGrammar(t *testing.T) {
	ok160 := strings.Repeat("a", 155) + "._:/-"
	for _, field := range []string{"request_id", "role_policy_version", "candidate_id", "access_id", "engine", "provider_id", "model_id", "transport", "variant", "capability_profile_version"} {
		for _, tc := range []struct {
			v  any
			ok bool
		}{{ok160, true}, {"A-z.0_9:/x", true}, {strings.Repeat("a", 161), false}, {"", false}, {"has space", false}, {"é", false}, {"a+b", false}, {"a@b", false}, {"a\nb", false}, {json.Number("1"), false}, {true, false}, {[]any{"a"}, false}} {
			m := testRequest(2)
			if strings.HasPrefix(field, "re") || field == "role_policy_version" {
				m[field] = tc.v
			} else {
				m["candidates"].([]any)[0].(map[string]any)[field] = tc.v
			}
			_, err := decodeMap(t, m)
			if tc.ok && err != nil {
				t.Fatalf("%s=%v rejected: %v", field, tc.v, err)
			}
			if !tc.ok {
				wantCode(t, err, field)
			}
		}
	}
	m := testRequest(2)
	m["candidates"].([]any)[0].(map[string]any)["variant"] = nil
	if req, err := decodeMap(t, m); err != nil || req.Candidates[0].Variant != nil {
		t.Fatalf("null variant: %v", err)
	}
	m = testRequest(1)
	m["candidates"].([]any)[0].(map[string]any)["candidate_id"] = "cand-ab"
	m["candidates"] = append(m["candidates"].([]any), testCandidate(1))
	_, err := decodeMap(t, m)
	wantCode(t, err, "duplicate_candidate_id")
}

func TestDecodeClosedKeys(t *testing.T) {
	for _, tc := range []struct {
		edit func(m map[string]any)
		code string
	}{
		{func(m map[string]any) { m["extra"] = "x" }, "unknown_field"},
		{func(m map[string]any) { delete(m, "state") }, "missing_field"},
		{func(m map[string]any) { m["candidates"].([]any)[0].(map[string]any)["extra"] = nil }, "unknown_field"},
		{func(m map[string]any) { delete(m["candidates"].([]any)[0].(map[string]any), "variant") }, "missing_field"},
		{func(m map[string]any) { m["candidates"] = map[string]any{} }, "candidates"},
		{func(m map[string]any) { m["candidates"] = []any{"x"} }, "candidates"},
		{func(m map[string]any) { m["role_brief"] = []any{} }, "role_brief"},
		{func(m map[string]any) { m["state"] = "x" }, "state"},
		{func(m map[string]any) { m["state"] = nil }, "state"},
		{func(m map[string]any) { m["candidates"].([]any)[0].(map[string]any)["description"] = nil }, "description"},
		{func(m map[string]any) { m["candidate_set_hash"] = strings.Repeat("A", 64) }, "candidate_set_hash"},
		{func(m map[string]any) { m["candidate_set_hash"] = strings.Repeat("a", 63) }, "candidate_set_hash"},
		{func(m map[string]any) { m["candidate_set_hash"] = strings.Repeat("a", 64) }, "hash_mismatch"},
	} {
		m := testRequest(2)
		tc.edit(m)
		_, err := decodeMap(t, m)
		wantCode(t, err, tc.code)
	}
}

func TestDecodeNumericTypes(t *testing.T) {
	base := encodeRequest(t, testRequest(1))
	for _, tc := range []struct{ old, new, code string }{
		{`"configuration_revision":1`, `"configuration_revision":1.0`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":1e0`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":1E0`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":-1`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":0`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":-0`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":01`, "syntax"},
		{`"configuration_revision":1`, `"configuration_revision":9007199254740992`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":99999999999999999999999`, "configuration_revision"},
		{`"configuration_revision":1`, `"configuration_revision":"1"`, "configuration_revision"},
		{`"schema_version":1`, `"schema_version":"1"`, "schema_version"},
		{`"schema_version":1`, `"schema_version":1.0`, "schema_version"},
		{`"schema_version":1`, `"schema_version":2`, "schema_version"},
		{`"schema_version":1`, `"schema_version":true`, "schema_version"},
		{`"role":"developer"`, `"role":1`, "state_number"},
		{`"role":"developer"`, `"role":["x",{"y":[0]}]`, "state_number"},
		{`"objective":"test"`, `"objective":1`, "role_brief_number"},
		{`"objective":"test"`, `"objective":{"a":[true,null,-2.5]}`, "role_brief_number"},
	} {
		in := bytes.Replace(base, []byte(tc.old), []byte(tc.new), 1)
		if bytes.Equal(in, base) {
			t.Fatalf("edit %s did not apply", tc.new)
		}
		_, err := Decode(bytes.NewReader(in))
		wantCode(t, err, tc.code)
	}
	m := testRequest(1)
	m["candidates"].([]any)[0].(map[string]any)["configuration_revision"] = json.Number("9007199254740991")
	req, err := decodeMap(t, m)
	if err != nil || req.Candidates[0].ConfigurationRevision != 9007199254740991 {
		t.Fatalf("max revision: %v", err)
	}
	h, _ := CandidateHash(req.Candidates)
	if h != rawHash(t, m["candidates"].([]any)) {
		t.Fatal("max revision hashed through a lossy path")
	}
}

func TestDecodeErrorsNeverEchoInput(t *testing.T) {
	const marker = "sk-live-PLANTED-MARKER-7f3a9c"
	base := encodeRequest(t, testRequest(1))
	for _, in := range [][]byte{
		bytes.Replace(base, []byte(`"request_id":"req-1"`), []byte(`"`+marker+`":"`+marker+`","request_id":"req-1"`), 1),
		bytes.Replace(base, []byte(`"request_id":"req-1"`), []byte(`"request_id":"`+marker+` x"`), 1),
		bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":"`+marker+`\ud800"`), 1),
		bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":"`+marker+`","role":"`+marker+`"`), 1),
		bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":`+marker), 1),
		bytes.Replace(base, []byte(`"role":"developer"`), []byte(`"role":"`+marker+"\xff"+`"`), 1),
		append(append([]byte{}, base...), marker...),
		[]byte(marker),
	} {
		_, err := Decode(bytes.NewReader(in))
		if !errors.Is(err, ErrInvalidRequest) || strings.Contains(err.Error(), "PLANTED") || strings.Contains(err.Error(), "sk-") {
			t.Fatalf("error leaks input or is untyped: %v", err)
		}
	}
}

func TestCanonicalGolden(t *testing.T) {
	s := "a\u2028b\u2029c\u0001d\u007fe<&>f\"g\\h\\u2028i\nj\tk\bl\fm\rn\u001fo🚀pé中\u0000"
	got, err := canonical(map[string]any{"s": s, "b": true, "n": nil, "i": json.Number("42"), "f": false, "a": []any{}, "o": map[string]any{}, "é": "x", "Z": "y", "中": "z", "z": "w"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Z":"y","a":[],"b":true,"f":false,"i":42,"n":null,"o":{},"s":"a` + "\u2028" + `b` + "\u2029" + `c\u0001d` + "\x7f" + `e<&>f\"g\\h\\u2028i\nj\tk\bl\fm\rn\u001fo🚀pé中\u0000","z":"w","é":"x","中":"z"}`
	if string(got) != want {
		t.Fatalf("canonical mismatch\n got %q\nwant %q", got, want)
	}
	for _, bad := range []any{1.5, float64(1), json.Number("1.5"), json.Number("-0"), json.Number("1e3"), json.Number("01"), json.Number("-12"), 1, "\xff", map[string]any{"\xff": "x"}, []string{"x"}, struct{}{}} {
		if _, err := canonical(bad); err == nil {
			t.Fatalf("canonical accepted %#v", bad)
		}
	}
	if b, err := canonical(int64(-12)); err != nil || string(b) != "-12" {
		t.Fatalf("negative integer: %s %v", b, err)
	}
}

func TestCandidateHashNullVariantAndOrder(t *testing.T) {
	c := []Candidate{{CandidateID: "c", AccessID: "a", Engine: "e", ProviderID: "p", ModelID: "m", Transport: "t", CapabilityProfileVersion: "v1", ConfigurationRevision: 7, Description: "d"}}
	want := `[{"access_id":"a","candidate_id":"c","capability_profile_version":"v1","configuration_revision":7,"description":"d","engine":"e","model_id":"m","provider_id":"p","transport":"t","variant":null}]`
	sum := sha256.Sum256([]byte(want))
	if h, err := CandidateHash(c); err != nil || h != hex.EncodeToString(sum[:]) {
		t.Fatalf("null variant hash %s %v", h, err)
	}
	empty := sha256.Sum256([]byte("[]"))
	if h, err := CandidateHash(nil); err != nil || h != hex.EncodeToString(empty[:]) {
		t.Fatalf("empty hash %s %v", h, err)
	}
	req := decodeFixture(t, "unicode.json")
	want0, err := CandidateHash(req.Candidates)
	if err != nil {
		t.Fatal(err)
	}
	cs := req.Candidates
	for _, perm := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		p := []Candidate{cs[perm[0]], cs[perm[1]], cs[perm[2]]}
		if h, err := CandidateHash(p); err != nil || h != want0 {
			t.Fatalf("hash depends on order %v", perm)
		}
	}
	if req.Candidates[0].CandidateID != "cand-c" {
		t.Fatal("CandidateHash reordered its input")
	}
	dup := []Candidate{cs[0], cs[0]}
	if _, err := CandidateHash(dup); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("hashed duplicate candidate IDs")
	}
	bad := append([]Candidate{}, cs...)
	bad[0].Description = "\xff"
	if _, err := CandidateHash(bad); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("hashed invalid UTF-8")
	}
}

func TestPythonReferenceHash(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		if os.Getenv(requirePythonEnv) == "1" {
			t.Fatal("python3 not on PATH but " + requirePythonEnv + "=1")
		}
		t.Skip("python3 not on PATH; set " + requirePythonEnv + "=1 to require it")
	}
	var files []string
	want := map[string]string{}
	for _, e := range readManifest(t) {
		if e.Kind == "request" && e.Valid {
			f := filepath.Join(fixtureDir, e.File)
			files = append(files, f)
			h, err := CandidateHash(decodeFixture(t, e.File).Candidates)
			if err != nil || h != *e.CandidateSetHash {
				t.Fatalf("%s: go hash %s", e.File, h)
			}
			want[f] = h
		}
	}
	if len(files) < 3 {
		t.Fatal("too few valid request fixtures")
	}
	out, err := exec.Command(py, append([]string{"-I", filepath.Join(fixtureDir, "reference_hash.py")}, files...)...).Output()
	if err != nil {
		t.Fatalf("python reference failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(files) {
		t.Fatalf("python printed %d lines", len(lines))
	}
	for i, line := range lines {
		if line != want[files[i]]+"  "+files[i] {
			t.Fatalf("python/go disagree: %q vs %s", line, want[files[i]])
		}
	}
	for _, name := range []string{"duplicate-key-top.json", "duplicate-key-escaped.json"} {
		bad := filepath.Join(fixtureDir, "invalid", name)
		if err := exec.Command(py, "-I", filepath.Join(fixtureDir, "reference_hash.py"), bad).Run(); err == nil {
			t.Fatalf("python reference accepted duplicate key in %s", name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestResponseWireOrder(t *testing.T) {
	b, err := json.Marshal(Response{SchemaVersion: 1, RequestID: "r", CandidateSetHash: "h", Status: StatusSelected, CandidateID: ptr("c"), Reason: ReasonClassified, Confidence: ptr(0.83), SelectorModel: ptr("jev-1.13.0"), LatencyMS: 312, Usage: &Usage{InputTokens: 512, OutputTokens: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"schema_version":1,"request_id":"r","candidate_set_hash":"h","status":"selected","candidate_id":"c","reason":"classified","confidence":0.83,"selector_model":"jev-1.13.0","latency_ms":312,"usage":{"input_tokens":512,"output_tokens":1}}` {
		t.Fatalf("wire order: %s", b)
	}
	b, _ = json.Marshal(Response{SchemaVersion: 1, Status: StatusAbstained, Reason: ReasonNoCandidates})
	if string(b) != `{"schema_version":1,"request_id":"","candidate_set_hash":"","status":"abstained","candidate_id":null,"reason":"no_candidates","confidence":null,"selector_model":null,"latency_ms":0,"usage":null}` {
		t.Fatalf("nulls not emitted: %s", b)
	}
}

func TestValidateResponseInvariants(t *testing.T) {
	reqs := map[int]Request{0: decodeFixture(t, "empty.json"), 1: decodeFixture(t, "singleton.json"), 3: decodeFixture(t, "unicode.json")}
	resp := func(n int, status, reason string, id *string, conf *float64, model *string, usage *Usage) Response {
		r := reqs[n]
		return Response{SchemaVersion: 1, RequestID: r.RequestID, CandidateSetHash: r.CandidateSetHash, Status: status, CandidateID: id, Reason: reason, Confidence: conf, SelectorModel: model, LatencyMS: 5, Usage: usage}
	}
	in1 := ptr(reqs[1].Candidates[0].CandidateID)
	in3 := ptr(reqs[3].Candidates[1].CandidateID)
	model := ptr("jev-1.13.0")
	u := &Usage{InputTokens: 10, OutputTokens: 1}
	valid := []struct {
		n int
		r Response
	}{
		{1, resp(1, StatusSelected, ReasonSingleton, in1, nil, nil, nil)},
		{0, resp(0, StatusAbstained, ReasonNoCandidates, nil, nil, nil, nil)},
		{3, resp(3, StatusSelected, ReasonClassified, in3, ptr(0.6), model, u)},
		{3, resp(3, StatusSelected, ReasonClassified, in3, ptr(1.0), model, nil)},
		{3, resp(3, StatusAbstained, ReasonLowConfidence, nil, ptr(0.0), model, u)},
		{3, resp(3, StatusError, ReasonInvalidRequest, nil, nil, nil, nil)},
		{3, resp(3, StatusError, ReasonInvalidResponse, nil, nil, model, u)},
		{3, resp(3, StatusError, ReasonInvalidResponse, nil, nil, nil, nil)},
		{3, resp(3, StatusSelected, ReasonClassified, in3, ptr(ConfidenceGate), model, u)},
		{3, resp(3, StatusAbstained, ReasonLowConfidence, nil, ptr(0.5999), model, u)},
		{3, resp(3, StatusSelected, ReasonClassified, in3, ptr(0.9), ptr("jev-0.0.0"), u)},
		{3, resp(3, StatusSelected, ReasonClassified, in3, ptr(0.9), ptr("jev-10.200.3000"), u)},
		{1, resp(1, StatusError, ReasonInvalidResponse, nil, nil, nil, nil)},
		{0, resp(0, StatusError, ReasonInvalidResponse, nil, nil, nil, nil)},
	}
	for _, reason := range []string{ReasonAuthentication, ReasonRateLimit, ReasonOverloaded, ReasonNetwork, ReasonTimeout, ReasonCancelled} {
		valid = append(valid, struct {
			n int
			r Response
		}{3, resp(3, StatusError, reason, nil, nil, nil, nil)})
	}
	for _, tc := range valid {
		if err := ValidateResponse(reqs[tc.n], tc.r); err != nil {
			t.Fatalf("valid %s/%s rejected: %v", tc.r.Status, tc.r.Reason, err)
		}
	}
	mod := func(r Response, f func(*Response)) Response { f(&r); return r }
	good := resp(3, StatusSelected, ReasonClassified, in3, ptr(0.83), model, u)
	invalid := []struct {
		name string
		n    int
		r    Response
		code string
	}{
		{"stale request_id", 3, mod(good, func(r *Response) { r.RequestID = "req-other" }), "request_id"},
		{"stale hash", 3, mod(good, func(r *Response) { r.CandidateSetHash = reqs[1].CandidateSetHash }), "candidate_set_hash"},
		{"schema", 3, mod(good, func(r *Response) { r.SchemaVersion = 2 }), "schema_version"},
		{"out of set", 3, mod(good, func(r *Response) { r.CandidateID = in1 }), "candidate_id"},
		{"selected null id", 3, mod(good, func(r *Response) { r.CandidateID = nil }), "candidate_id"},
		{"unknown status", 3, mod(good, func(r *Response) { r.Status = "chosen" }), "status"},
		{"unknown reason", 3, mod(good, func(r *Response) { r.Reason = "guess" }), "reason"},
		{"status/reason mismatch", 3, mod(good, func(r *Response) { r.Status = StatusAbstained; r.CandidateID = nil }), "status"},
		{"confidence 1.5", 3, mod(good, func(r *Response) { r.Confidence = ptr(1.5) }), "confidence"},
		{"confidence -0.1", 3, mod(good, func(r *Response) { r.Confidence = ptr(-0.1) }), "confidence"},
		{"confidence NaN", 3, mod(good, func(r *Response) { r.Confidence = ptr(math.NaN()) }), "confidence"},
		{"confidence Inf", 3, mod(good, func(r *Response) { r.Confidence = ptr(math.Inf(1)) }), "confidence"},
		{"classified null confidence", 3, mod(good, func(r *Response) { r.Confidence = nil }), "missing_inference"},
		{"classified null model", 3, mod(good, func(r *Response) { r.SelectorModel = nil }), "missing_inference"},
		{"bad model id", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("jev 1") }), "selector_model"},
		{"negative latency", 3, mod(good, func(r *Response) { r.LatencyMS = -1 }), "latency_ms"},
		{"negative usage", 3, mod(good, func(r *Response) { r.Usage = &Usage{InputTokens: -1} }), "usage"},
		{"classified on singleton", 1, resp(1, StatusSelected, ReasonClassified, in1, ptr(0.9), model, nil), "reason_for_candidate_count"},
		{"singleton on 3", 3, resp(3, StatusSelected, ReasonSingleton, in3, nil, nil, nil), "reason_for_candidate_count"},
		{"singleton with confidence", 1, resp(1, StatusSelected, ReasonSingleton, in1, ptr(1.0), nil, nil), "confidence"},
		{"singleton with model", 1, resp(1, StatusSelected, ReasonSingleton, in1, nil, model, nil), "unexpected_inference"},
		{"singleton with usage", 1, resp(1, StatusSelected, ReasonSingleton, in1, nil, nil, u), "unexpected_inference"},
		{"no_candidates on 3", 3, resp(3, StatusAbstained, ReasonNoCandidates, nil, nil, nil, nil), "reason_for_candidate_count"},
		{"abstained with id", 0, resp(0, StatusAbstained, ReasonNoCandidates, ptr("cand-a"), nil, nil, nil), "candidate_id"},
		{"low_confidence with id", 3, resp(3, StatusAbstained, ReasonLowConfidence, in3, ptr(0.2), model, nil), "candidate_id"},
		{"low_confidence null confidence", 3, resp(3, StatusAbstained, ReasonLowConfidence, nil, nil, model, nil), "missing_inference"},
		{"low_confidence on singleton", 1, resp(1, StatusAbstained, ReasonLowConfidence, nil, ptr(0.2), model, nil), "reason_for_candidate_count"},
		{"error with id", 3, resp(3, StatusError, ReasonTimeout, in3, nil, nil, nil), "candidate_id"},
		{"error with confidence", 3, resp(3, StatusError, ReasonInvalidResponse, nil, ptr(0.5), model, nil), "confidence"},
		{"timeout with model", 3, resp(3, StatusError, ReasonTimeout, nil, nil, model, nil), "unexpected_inference"},
		{"network with usage", 3, resp(3, StatusError, ReasonNetwork, nil, nil, nil, u), "unexpected_inference"},
		{"invalid_request on singleton", 1, resp(1, StatusError, ReasonInvalidRequest, nil, nil, nil, nil), "reason_for_candidate_count"},
		{"reason under wrong status", 3, resp(3, StatusSelected, ReasonTimeout, in3, nil, nil, nil), "status"},
		{"classified below gate", 3, mod(good, func(r *Response) { r.Confidence = ptr(0.5999) }), "confidence_gate"},
		{"classified at zero", 3, mod(good, func(r *Response) { r.Confidence = ptr(0.0) }), "confidence_gate"},
		{"low_confidence at gate", 3, resp(3, StatusAbstained, ReasonLowConfidence, nil, ptr(ConfidenceGate), model, u), "confidence_gate"},
		{"low_confidence at one", 3, resp(3, StatusAbstained, ReasonLowConfidence, nil, ptr(1.0), model, u), "confidence_gate"},
		{"empty request_id", 3, mod(good, func(r *Response) { r.RequestID = "" }), "request_id"},
		{"empty hash", 3, mod(good, func(r *Response) { r.CandidateSetHash = "" }), "candidate_set_hash"},
		{"model alias latest", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("jev-latest") }), "selector_model"},
		{"model alias preview", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("jev-preview") }), "selector_model"},
		{"model two parts", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("jev-1.13") }), "selector_model"},
		{"model suffix", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("jev-1.13.0-rc1") }), "selector_model"},
		{"model other family", 3, mod(good, func(r *Response) { r.SelectorModel = ptr("gpt-1.13.0") }), "selector_model"},
		{"low_confidence alias model", 3, resp(3, StatusAbstained, ReasonLowConfidence, nil, ptr(0.2), ptr("jev-latest"), u), "selector_model"},
		{"invalid_response alias model", 3, resp(3, StatusError, ReasonInvalidResponse, nil, nil, ptr("jev-latest"), u), "selector_model"},
		{"invalid_response model on singleton", 1, resp(1, StatusError, ReasonInvalidResponse, nil, nil, model, nil), "unexpected_inference"},
		{"invalid_response usage on singleton", 1, resp(1, StatusError, ReasonInvalidResponse, nil, nil, nil, u), "unexpected_inference"},
		{"invalid_response model on empty", 0, resp(0, StatusError, ReasonInvalidResponse, nil, nil, model, u), "unexpected_inference"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			wantResponseCode(t, ValidateResponse(reqs[tc.n], tc.r), tc.code)
		})
	}
}

func TestValidateResponseRequiresIdentity(t *testing.T) {
	zero := Response{SchemaVersion: SchemaVersion, Status: StatusAbstained, Reason: ReasonNoCandidates}
	wantResponseCode(t, ValidateResponse(Request{}, zero), "request_id")
	idOnly := zero
	idOnly.RequestID = "req-1"
	wantResponseCode(t, ValidateResponse(Request{RequestID: "req-1"}, idOnly), "candidate_set_hash")
	upper := idOnly
	upper.CandidateSetHash = strings.Repeat("A", 64)
	wantResponseCode(t, ValidateResponse(Request{RequestID: "req-1", CandidateSetHash: upper.CandidateSetHash}, upper), "candidate_set_hash")
	badID := zero
	badID.RequestID, badID.CandidateSetHash = "req 1", strings.Repeat("a", 64)
	wantResponseCode(t, ValidateResponse(Request{RequestID: "req 1", CandidateSetHash: badID.CandidateSetHash}, badID), "request_id")
}

func TestValidJevModel(t *testing.T) {
	for _, s := range []string{"jev-1.13.0", "jev-0.0.0", "jev-10.200.3000", "jev-01.2.3"} {
		if !ValidJevModel(s) {
			t.Errorf("%q rejected", s)
		}
	}
	for _, s := range []string{"", "jev-latest", "jev-preview", "jev-1.13", "jev-1.13.0.1", "jev-1.13.0-rc1", "Jev-1.13.0", "jev-1..0", "jev-.1.0", "jev-1.13.", " jev-1.13.0", "jev-1.13.0 ", "jev-1.13.0\n", "jev-a.b.c", "jev--1.0.0", "jev-1.\u0661.0", "xjev-1.13.0"} {
		if ValidJevModel(s) {
			t.Errorf("%q accepted", s)
		}
	}
}
