// Package selection implements the closed mroute selection protocol (docs/contracts/selection-v1.md).
package selection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"unicode/utf8"
)

const (
	SchemaVersion            = 1
	MaxRequestBytes          = 65536
	MaxRoleBriefBytes        = 4096
	MaxStateBytes            = 12288
	MaxCandidates            = 32
	MaxDescriptionBytes      = 1024
	MaxIDBytes               = 160
	MaxStructuredDepth       = 32
	MaxConfigurationRevision = 9007199254740991
)

const (
	StatusSelected  = "selected"
	StatusAbstained = "abstained"
	StatusError     = "error"

	ReasonSingleton       = "singleton"
	ReasonClassified      = "classified"
	ReasonNoCandidates    = "no_candidates"
	ReasonLowConfidence   = "low_confidence"
	ReasonInvalidRequest  = "invalid_request"
	ReasonInvalidResponse = "invalid_response"
	ReasonAuthentication  = "authentication"
	ReasonRateLimit       = "rate_limit"
	ReasonOverloaded      = "overloaded"
	ReasonNetwork         = "network"
	ReasonTimeout         = "timeout"
	ReasonCancelled       = "cancelled"
)

// Errors carry only fixed codes, never input content.
var (
	ErrInvalidRequest  = errors.New("invalid_request")
	ErrInvalidResponse = errors.New("invalid_response")
	errCanonical       = errors.New("selection: value has no canonical encoding")
)

type Request struct {
	SchemaVersion     int             `json:"schema_version"`
	RequestID         string          `json:"request_id"`
	CandidateSetHash  string          `json:"candidate_set_hash"`
	RolePolicyVersion string          `json:"role_policy_version"`
	RoleBrief         json.RawMessage `json:"role_brief"`
	State             json.RawMessage `json:"state"`
	Candidates        []Candidate     `json:"candidates"`
}
type Candidate struct {
	CandidateID              string  `json:"candidate_id"`
	AccessID                 string  `json:"access_id"`
	Engine                   string  `json:"engine"`
	ProviderID               string  `json:"provider_id"`
	ModelID                  string  `json:"model_id"`
	Transport                string  `json:"transport"`
	Variant                  *string `json:"variant"`
	CapabilityProfileVersion string  `json:"capability_profile_version"`
	ConfigurationRevision    int64   `json:"configuration_revision"`
	Description              string  `json:"description"`
}
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
type Response struct {
	SchemaVersion    int      `json:"schema_version"`
	RequestID        string   `json:"request_id"`
	CandidateSetHash string   `json:"candidate_set_hash"`
	Status           string   `json:"status"`
	CandidateID      *string  `json:"candidate_id"`
	Reason           string   `json:"reason"`
	Confidence       *float64 `json:"confidence"`
	SelectorModel    *string  `json:"selector_model"`
	LatencyMS        int64    `json:"latency_ms"`
	Usage            *Usage   `json:"usage"`
}

var (
	requestKeys   = []string{"schema_version", "request_id", "candidate_set_hash", "role_policy_version", "role_brief", "state", "candidates"}
	candidateKeys = []string{"candidate_id", "access_id", "engine", "provider_id", "model_id", "transport", "variant", "capability_profile_version", "configuration_revision", "description"}
)

func invalid(code string) error { return fmt.Errorf("%w: %s", ErrInvalidRequest, code) }

// Decode reads and validates exactly one request; every rejection wraps ErrInvalidRequest.
func Decode(r io.Reader) (Request, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxRequestBytes+1))
	if err != nil {
		return Request{}, invalid("read")
	}
	if len(b) > MaxRequestBytes {
		return Request{}, invalid("too_large")
	}
	return decodeBytes(b)
}

func decodeBytes(b []byte) (Request, error) {
	if bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		return Request{}, invalid("bom")
	}
	if !utf8.Valid(b) {
		return Request{}, invalid("invalid_utf8")
	}
	if !pairedSurrogates(b) {
		return Request{}, invalid("surrogate")
	}
	v, err := parseDocument(b)
	if err != nil {
		return Request{}, err
	}
	top, ok := v.(map[string]any)
	if !ok {
		return Request{}, invalid("not_object")
	}
	if err := closedKeys(top, requestKeys); err != nil {
		return Request{}, err
	}
	var req Request
	if n, ok := top["schema_version"].(json.Number); !ok || n != "1" {
		return Request{}, invalid("schema_version")
	}
	req.SchemaVersion = SchemaVersion
	if req.RequestID, err = idField(top, "request_id"); err != nil {
		return Request{}, err
	}
	if req.RolePolicyVersion, err = idField(top, "role_policy_version"); err != nil {
		return Request{}, err
	}
	if req.RoleBrief, err = structured(top, "role_brief", MaxRoleBriefBytes); err != nil {
		return Request{}, err
	}
	if req.State, err = structured(top, "state", MaxStateBytes); err != nil {
		return Request{}, err
	}
	list, ok := top["candidates"].([]any)
	if !ok {
		return Request{}, invalid("candidates")
	}
	if len(list) > MaxCandidates {
		return Request{}, invalid("too_many_candidates")
	}
	req.Candidates = make([]Candidate, 0, len(list))
	for _, item := range list {
		c, err := candidateFrom(item)
		if err != nil {
			return Request{}, err
		}
		req.Candidates = append(req.Candidates, c)
	}
	if err := validateCandidates(req.Candidates); err != nil {
		return Request{}, err
	}
	hash, ok := top["candidate_set_hash"].(string)
	if !ok || !isLowerHex64(hash) {
		return Request{}, invalid("candidate_set_hash")
	}
	computed, err := CandidateHash(req.Candidates)
	if err != nil || computed != hash {
		return Request{}, invalid("hash_mismatch")
	}
	req.CandidateSetHash = hash
	return req, nil
}

// parseDocument decodes one JSON value, rejecting duplicate keys at any depth and any trailing value.
func parseDocument(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := parseValue(dec, 1)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("trailing_data")
	}
	return v, nil
}

// The request object is depth 1, so role_brief/state level 32 is document depth 33.
func parseValue(dec *json.Decoder, depth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, invalid("syntax")
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if depth > MaxStructuredDepth+1 {
		return nil, invalid("too_deep")
	}
	switch d {
	case '{':
		m := map[string]any{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, invalid("syntax")
			}
			k, ok := kt.(string)
			if !ok {
				return nil, invalid("syntax")
			}
			if _, dup := m[k]; dup {
				return nil, invalid("duplicate_key")
			}
			if m[k], err = parseValue(dec, depth+1); err != nil {
				return nil, err
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, invalid("syntax")
		}
		return m, nil
	case '[':
		a := []any{}
		for dec.More() {
			v, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, invalid("syntax")
		}
		return a, nil
	}
	return nil, invalid("syntax")
}

// pairedSurrogates scans raw string escapes, because encoding/json silently maps unpaired surrogates to U+FFFD.
func pairedSurrogates(b []byte) bool {
	in := false
	for i := 0; i < len(b); i++ {
		if !in {
			in = b[i] == '"'
			continue
		}
		switch b[i] {
		case '"':
			in = false
		case '\\':
			if i+1 < len(b) && b[i+1] != 'u' {
				i++
				continue
			}
			r, ok := hex4(b, i+2)
			if !ok {
				return true // malformed escape; the decoder rejects it
			}
			i += 5
			if r >= 0xdc00 && r <= 0xdfff {
				return false
			}
			if r >= 0xd800 && r <= 0xdbff {
				if i+2 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
					return false
				}
				lo, ok := hex4(b, i+3)
				if !ok || lo < 0xdc00 || lo > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

func hex4(b []byte, at int) (rune, bool) {
	if at < 0 || at+4 > len(b) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(b[at:at+4]), 16, 32)
	return rune(n), err == nil
}

func closedKeys(m map[string]any, keys []string) error {
	for k := range m {
		if !slices.Contains(keys, k) {
			return invalid("unknown_field")
		}
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return invalid("missing_field")
		}
	}
	return nil
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > MaxIDBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '/' || c == '-') {
			return false
		}
	}
	return true
}

func idField(m map[string]any, key string) (string, error) {
	s, ok := m[key].(string)
	if !ok || !validID(s) {
		return "", invalid(key)
	}
	return s, nil
}

func structured(m map[string]any, key string, limit int) (json.RawMessage, error) {
	obj, ok := m[key].(map[string]any)
	if !ok {
		return nil, invalid(key)
	}
	if hasNumber(obj) {
		return nil, invalid(key + "_number")
	}
	b, err := Canonical(obj)
	if err != nil {
		return nil, invalid(key)
	}
	if len(b) > limit {
		return nil, invalid(key + "_size")
	}
	return b, nil
}

func hasNumber(v any) bool {
	switch x := v.(type) {
	case json.Number:
		return true
	case map[string]any:
		for _, e := range x {
			if hasNumber(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasNumber(e) {
				return true
			}
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" || (s[0] == '0' && len(s) > 1) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func candidateFrom(v any) (Candidate, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Candidate{}, invalid("candidates")
	}
	if err := closedKeys(m, candidateKeys); err != nil {
		return Candidate{}, err
	}
	var c Candidate
	for key, dst := range map[string]*string{"candidate_id": &c.CandidateID, "access_id": &c.AccessID, "engine": &c.Engine, "provider_id": &c.ProviderID, "model_id": &c.ModelID, "transport": &c.Transport, "capability_profile_version": &c.CapabilityProfileVersion, "description": &c.Description} {
		s, ok := m[key].(string)
		if !ok {
			return Candidate{}, invalid(key)
		}
		*dst = s
	}
	switch x := m["variant"].(type) {
	case nil:
	case string:
		c.Variant = &x
	default:
		return Candidate{}, invalid("variant")
	}
	n, ok := m["configuration_revision"].(json.Number)
	if !ok || !isDigits(string(n)) {
		return Candidate{}, invalid("configuration_revision")
	}
	rev, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return Candidate{}, invalid("configuration_revision")
	}
	c.ConfigurationRevision = rev
	return c, nil
}

func validateCandidates(cs []Candidate) error {
	if len(cs) > MaxCandidates {
		return invalid("too_many_candidates")
	}
	seen := make(map[string]bool, len(cs))
	for _, c := range cs {
		for _, f := range []struct{ key, v string }{{"candidate_id", c.CandidateID}, {"access_id", c.AccessID}, {"engine", c.Engine}, {"provider_id", c.ProviderID}, {"model_id", c.ModelID}, {"transport", c.Transport}, {"capability_profile_version", c.CapabilityProfileVersion}} {
			if !validID(f.v) {
				return invalid(f.key)
			}
		}
		if c.Variant != nil && !validID(*c.Variant) {
			return invalid("variant")
		}
		if c.ConfigurationRevision < 1 || c.ConfigurationRevision > MaxConfigurationRevision {
			return invalid("configuration_revision")
		}
		if len(c.Description) == 0 || len(c.Description) > MaxDescriptionBytes || !utf8.ValidString(c.Description) {
			return invalid("description")
		}
		if seen[c.CandidateID] {
			return invalid("duplicate_candidate_id")
		}
		seen[c.CandidateID] = true
	}
	return nil
}

func (c Candidate) value() map[string]any {
	var variant any
	if c.Variant != nil {
		variant = *c.Variant
	}
	return map[string]any{"candidate_id": c.CandidateID, "access_id": c.AccessID, "engine": c.Engine, "provider_id": c.ProviderID, "model_id": c.ModelID, "transport": c.Transport, "variant": variant, "capability_profile_version": c.CapabilityProfileVersion, "configuration_revision": c.ConfigurationRevision, "description": c.Description}
}

// CandidateHash is the lowercase hex SHA-256 of the canonical candidate array sorted by candidate_id.
func CandidateHash(cs []Candidate) (string, error) {
	if err := validateCandidates(cs); err != nil {
		return "", err
	}
	sorted := slices.Clone(cs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CandidateID < sorted[j].CandidateID })
	arr := make([]any, len(sorted))
	for i, c := range sorted {
		arr[i] = c.value()
	}
	b, err := Canonical(arr)
	if err != nil {
		return "", invalid("candidates")
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Canonical encodes generic JSON values exactly like Python
// json.dumps(v, ensure_ascii=False, sort_keys=True, separators=(",", ":")).
// Only integers are accepted as numbers.
func Canonical(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		return writeString(b, x)
	case json.Number:
		s := string(x)
		if len(s) > 0 && s[0] == '-' {
			s = s[1:]
		}
		if !isDigits(s) || x == "-0" {
			return errCanonical
		}
		b.WriteString(string(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case int:
		b.WriteString(strconv.Itoa(x))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeCanonical(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return errCanonical
	}
	return nil
}

func writeString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return errCanonical
	}
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

type reasonRule struct {
	status    string
	min, max  int
	inference bool
}

var reasonRules = map[string]reasonRule{
	ReasonSingleton:       {StatusSelected, 1, 1, false},
	ReasonClassified:      {StatusSelected, 2, MaxCandidates, true},
	ReasonNoCandidates:    {StatusAbstained, 0, 0, false},
	ReasonLowConfidence:   {StatusAbstained, 2, MaxCandidates, true},
	ReasonInvalidRequest:  {StatusError, 2, MaxCandidates, false},
	ReasonInvalidResponse: {StatusError, 0, MaxCandidates, false},
	ReasonAuthentication:  {StatusError, 0, MaxCandidates, false},
	ReasonRateLimit:       {StatusError, 0, MaxCandidates, false},
	ReasonOverloaded:      {StatusError, 0, MaxCandidates, false},
	ReasonNetwork:         {StatusError, 0, MaxCandidates, false},
	ReasonTimeout:         {StatusError, 0, MaxCandidates, false},
	ReasonCancelled:       {StatusError, 0, MaxCandidates, false},
}

// ValidateResponse checks a response against the request it must answer, including request identity.
func ValidateResponse(req Request, resp Response) error {
	bad := func(code string) error { return fmt.Errorf("%w: %s", ErrInvalidResponse, code) }
	if resp.SchemaVersion != SchemaVersion {
		return bad("schema_version")
	}
	if resp.RequestID != req.RequestID {
		return bad("request_id")
	}
	if resp.CandidateSetHash != req.CandidateSetHash {
		return bad("candidate_set_hash")
	}
	rule, ok := reasonRules[resp.Reason]
	if !ok {
		return bad("reason")
	}
	if resp.Status != rule.status {
		return bad("status")
	}
	if n := len(req.Candidates); n < rule.min || n > rule.max {
		return bad("reason_for_candidate_count")
	}
	if resp.Status == StatusSelected {
		if resp.CandidateID == nil || !slices.ContainsFunc(req.Candidates, func(c Candidate) bool { return c.CandidateID == *resp.CandidateID }) {
			return bad("candidate_id")
		}
	} else if resp.CandidateID != nil {
		return bad("candidate_id")
	}
	if c := resp.Confidence; c != nil && (math.IsNaN(*c) || math.IsInf(*c, 0) || *c < 0 || *c > 1) {
		return bad("confidence")
	}
	if resp.SelectorModel != nil && !validID(*resp.SelectorModel) {
		return bad("selector_model")
	}
	if resp.LatencyMS < 0 {
		return bad("latency_ms")
	}
	if u := resp.Usage; u != nil && (u.InputTokens < 0 || u.OutputTokens < 0) {
		return bad("usage")
	}
	switch {
	case rule.inference && (resp.Confidence == nil || resp.SelectorModel == nil):
		return bad("missing_inference")
	case !rule.inference && resp.Confidence != nil:
		return bad("confidence")
	case !rule.inference && resp.Reason != ReasonInvalidResponse && (resp.SelectorModel != nil || resp.Usage != nil):
		return bad("unexpected_inference")
	}
	return nil
}
