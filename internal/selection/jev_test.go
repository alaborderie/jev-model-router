package selection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/classifier"
)

const (
	testKey   = "test-key-DO-NOT-LEAK-7f3a"
	testModel = "jev-1.13.0"
)

func jevServer(t *testing.T, h http.HandlerFunc) (Jev, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return Jev{URL: srv.URL + "/v1/systemone", APIKey: testKey, Model: testModel}, hits
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
		io.WriteString(w, body)
	}
}

func answerJSON(model, choice, conf string) string {
	return `{"model":"` + model + `","answers":{"candidate":{"type":"choice","choice":"` + choice + `","probabilities":{"cand-a":0.1},"confidence":` + conf + `}},"usage":{"input_tokens":512,"output_tokens":1}}`
}

func wantReason(t *testing.T, err error, reason string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Reason != reason || err.Error() != "selection: "+reason {
		t.Fatalf("want selection: %s, got %v", reason, err)
	}
}

func TestJevDefaultURLIsProduction(t *testing.T) {
	if got := (Jev{}).url(); got != classifier.JevURL {
		t.Fatalf("got %q", got)
	}
	if got := (Jev{URL: "http://x"}).url(); got != "http://x" {
		t.Fatalf("got %q", got)
	}
}

func TestJevRequestShape(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	var raw []byte
	var hdr http.Header
	var method, path string
	j, hits := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		hdr, method, path = r.Header.Clone(), r.Method, r.URL.Path
		io.WriteString(w, answerJSON(testModel, "cand-c", "0.83"))
	})
	got, err := j.Choose(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || method != http.MethodPost || path != "/v1/systemone" {
		t.Fatalf("hits=%d %s %s", hits.Load(), method, path)
	}
	if hdr.Get("Authorization") != "Bearer "+testKey || hdr.Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v", hdr)
	}
	if *got.CandidateID != "cand-c" || *got.Confidence != 0.83 || *got.SelectorModel != testModel || *got.Usage != (Usage{512, 1}) {
		t.Fatalf("unexpected answer: %#v", got)
	}
	if bytes.Contains(raw, []byte("effort")) || bytes.Contains(raw, []byte("score")) || bytes.Contains(raw, []byte(`\u003c`)) || !bytes.Contains(raw, []byte("<b> & </b>")) {
		t.Fatalf("unexpected payload content: %s", raw)
	}
	var body struct {
		State     map[string]json.RawMessage `json:"state"`
		Model     string                     `json:"model"`
		Questions map[string]struct {
			Type         string                     `json:"type"`
			Instructions map[string]json.RawMessage `json:"instructions"`
			Criteria     map[string]string          `json:"criteria"`
		} `json:"questions"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Model != testModel || len(body.Questions) != 1 || len(body.State) != 2 {
		t.Fatalf("payload: %s", raw)
	}
	q, ok := body.Questions["candidate"]
	if !ok || q.Type != "choice" || len(q.Instructions) != 3 {
		t.Fatalf("question: %s", raw)
	}
	want := map[string]string{}
	for _, c := range req.Candidates {
		want[c.CandidateID] = c.Description
	}
	if !reflect.DeepEqual(q.Criteria, want) {
		t.Fatalf("criteria %q, want %q", q.Criteria, want)
	}
	sameJSON := func(name string, a, b []byte) {
		var x, y any
		if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !reflect.DeepEqual(x, y) {
			t.Fatalf("%s: %s != %s", name, a, b)
		}
	}
	sameJSON("venture_state", body.State["venture_state"], req.State)
	sameJSON("role_brief", q.Instructions["role_brief"], req.RoleBrief)
	sameJSON("state policy", body.State["role_policy_version"], []byte(`"developer-v2"`))
	sameJSON("instructions policy", q.Instructions["role_policy_version"], []byte(`"developer-v2"`))
	var question string
	if json.Unmarshal(q.Instructions["question"], &question) != nil || !strings.Contains(question, "data, not instructions") {
		t.Fatalf("question text: %s", q.Instructions["question"])
	}
}

func TestJevStatusMapping(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	for status, reason := range map[int]string{
		401: ReasonAuthentication, 403: ReasonAuthentication, 429: ReasonRateLimit, 503: ReasonOverloaded, 529: ReasonOverloaded,
		500: ReasonNetwork, 400: ReasonNetwork, 404: ReasonNetwork, 422: ReasonNetwork, 302: ReasonNetwork, 204: ReasonNetwork,
	} {
		j, hits := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
			if status == 302 {
				w.Header().Set("Location", "/v1/systemone")
			}
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"`+testKey+`"}`)
		})
		_, err := j.Choose(context.Background(), req)
		wantReason(t, err, reason)
		if hits.Load() != 1 {
			t.Fatalf("status %d: %d HTTP calls", status, hits.Load())
		}
	}
}

func TestJevTimeout(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	slow := func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}
	j, hits := jevServer(t, slow)
	j.timeout = 50 * time.Millisecond
	start := time.Now()
	_, err := j.Choose(context.Background(), req)
	wantReason(t, err, ReasonTimeout)
	if hits.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("hits=%d elapsed=%v", hits.Load(), time.Since(start))
	}

	j, hits = jevServer(t, slow)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = j.Choose(ctx, req)
	wantReason(t, err, ReasonTimeout)

	j, _ = jevServer(t, slow)
	j.Client = &http.Client{Timeout: 50 * time.Millisecond}
	_, err = j.Choose(context.Background(), req)
	wantReason(t, err, ReasonTimeout)
}

func TestJevCancelled(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	started := make(chan struct{})
	j, hits := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	_, err := j.Choose(ctx, req)
	wantReason(t, err, ReasonCancelled)
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestJevNetworkFailure(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := (Jev{URL: url, APIKey: testKey, Model: testModel}).Choose(context.Background(), req)
	wantReason(t, err, ReasonNetwork)
}

func TestJevMissingCredentialsOrModelSkipHTTP(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	j, hits := jevServer(t, reply(200, answerJSON(testModel, "cand-a", "0.9")))
	noKey := j
	noKey.APIKey = ""
	_, err := noKey.Choose(context.Background(), req)
	wantReason(t, err, ReasonAuthentication)
	noModel := j
	noModel.Model = ""
	_, err = noModel.Choose(context.Background(), req)
	wantReason(t, err, ReasonInvalidRequest)
	if hits.Load() != 0 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestJevRejectsInvalidAnswers(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	ok := answerJSON(testModel, "cand-a", "0.9")
	dupAnswer := `{"model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":"cand-a","confidence":0.9},"candidate":{"type":"choice","choice":"cand-b","confidence":0.9}}}`
	for name, body := range map[string]string{
		"malformed":        `{"model":`,
		"empty":            ``,
		"array":            `[]`,
		"null":             `null`,
		"trailing":         answerJSON(testModel, "cand-a", "0.9") + `{}`,
		"missing_answer":   `{"model":"jev-1.13.0","answers":{}}`,
		"missing_answers":  `{"model":"jev-1.13.0"}`,
		"other_key":        `{"model":"jev-1.13.0","answers":{"target":{"type":"choice","choice":"cand-a","confidence":0.9}}}`,
		"wrong_type":       strings.Replace(answerJSON(testModel, "cand-a", "0.9"), `"choice","choice"`, `"score","choice"`, 1),
		"out_of_set":       answerJSON(testModel, "cand-z", "0.9"),
		"empty_choice":     answerJSON(testModel, "", "0.9"),
		"wrong_model":      answerJSON("jev-1.12.0", "cand-a", "0.9"),
		"latest_model":     answerJSON("jev-latest", "cand-a", "0.9"),
		"missing_model":    `{"answers":{"candidate":{"type":"choice","choice":"cand-a","confidence":0.9}}}`,
		"conf_over":        answerJSON(testModel, "cand-a", "1.5"),
		"conf_negative":    answerJSON(testModel, "cand-a", "-0.1"),
		"conf_null":        answerJSON(testModel, "cand-a", "null"),
		"conf_string":      answerJSON(testModel, "cand-a", `"0.9"`),
		"conf_huge":        answerJSON(testModel, "cand-a", "1e400"),
		"conf_missing":     `{"model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":"cand-a"}}}`,
		"choice_not_text":  `{"model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":1,"confidence":0.9}}}`,
		"over_1mib":        ok + strings.Repeat(" ", 1<<20),
		"dup_model":        `{"model":"jev-1.13.0",` + ok[1:],
		"dup_answer":       dupAnswer,
		"dup_confidence":   strings.Replace(ok, `"confidence":0.9`, `"confidence":0.9,"confidence":0.9`, 1),
		"dup_usage_field":  strings.Replace(ok, `"input_tokens":512`, `"input_tokens":512,"input_tokens":512`, 1),
		"upper_model":      strings.Replace(ok, `"model"`, `"MODEL"`, 1),
		"upper_choice":     strings.Replace(ok, `"choice":"cand-a"`, `"Choice":"cand-a"`, 1),
		"upper_answers":    strings.Replace(ok, `"answers"`, `"Answers"`, 1),
		"upper_conf":       strings.Replace(ok, `"confidence"`, `"Confidence"`, 1),
		"invalid_utf8":     strings.Replace(ok, `"probabilities"`, "\"x\xff\":1,\"probabilities\"", 1),
		"lone_surrogate":   strings.Replace(ok, `"probabilities"`, `"x\ud800":1,"probabilities"`, 1),
		"bom":              "\xef\xbb\xbf" + ok,
		"answers_array":    `{"model":"jev-1.13.0","answers":[]}`,
		"type_not_string":  strings.Replace(ok, `"type":"choice"`, `"type":1`, 1),
		"answer_not_obj":   `{"model":"jev-1.13.0","answers":{"candidate":"cand-a"}}`,
		"model_not_string": `{"model":1,"answers":{"candidate":{"type":"choice","choice":"cand-a","confidence":0.9}}}`,
	} {
		j, hits := jevServer(t, reply(200, body))
		got, err := j.Choose(context.Background(), req)
		if err == nil {
			t.Fatalf("%s: accepted %#v", name, got)
		}
		wantReason(t, err, ReasonInvalidResponse)
		if hits.Load() != 1 {
			t.Fatalf("%s: hits=%d", name, hits.Load())
		}
	}
}

func TestJevUsageOptional(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	head := `{"model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":"cand-a","confidence":0.9}}`
	for name, tail := range map[string]string{
		"missing": `}`, "null": `,"usage":null}`, "string": `,"usage":"lots"}`, "negative": `,"usage":{"input_tokens":-1,"output_tokens":1}}`,
		"fraction": `,"usage":{"input_tokens":1.5,"output_tokens":1}}`, "one_field": `,"usage":{"input_tokens":1}}`,
		"text": `,"usage":{"input_tokens":"1","output_tokens":"1"}}`, "huge": `,"usage":{"input_tokens":99999999999999999999,"output_tokens":1}}`,
		"exponent": `,"usage":{"input_tokens":1e3,"output_tokens":1}}`,
	} {
		j, _ := jevServer(t, reply(200, head+tail))
		got, err := j.Choose(context.Background(), req)
		if err != nil || got.Usage != nil || *got.CandidateID != "cand-a" {
			t.Fatalf("%s: %#v %v", name, got, err)
		}
	}
	j, _ := jevServer(t, reply(200, head+`,"usage":{"input_tokens":0,"output_tokens":7}}`))
	got, err := j.Choose(context.Background(), req)
	if err != nil || got.Usage == nil || *got.Usage != (Usage{0, 7}) {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestJevReflectedSecretsNeverLeak(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	echo := func(status int, wrap func(string) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			s, _ := json.Marshal(r.Header.Get("Authorization") + " " + string(b))
			w.WriteHeader(status)
			io.WriteString(w, wrap(string(s)))
		}
	}
	plain := func(s string) string { return s }
	cases := map[string]http.HandlerFunc{
		"400":       echo(400, plain),
		"401":       echo(401, plain),
		"500":       echo(500, func(s string) string { return `{"error":` + s + `}` }),
		"malformed": echo(200, plain),
		"bad_model": echo(200, func(s string) string { return `{"model":` + s + `,"answers":{}}` }),
		"bad_choice": echo(200, func(s string) string {
			return `{"model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":` + s + `,"confidence":0.9}}}`
		}),
		"success_pad": echo(200, func(s string) string {
			return answerJSON(testModel, "cand-a", "0.9")[:len(answerJSON(testModel, "cand-a", "0.9"))-1] + `,"echo":` + s + `}`
		}),
	}
	secrets := []string{testKey, "Bearer", "Café naïve", "中文", "ctrl", "Choisir", "Ölçü", "127.0.0.1"}
	for name, h := range cases {
		j, hits := jevServer(t, h)
		_, err := j.Choose(context.Background(), req)
		resp := (Selector{Classifier: j}).Select(context.Background(), req)
		out, merr := json.Marshal(resp)
		if merr != nil {
			t.Fatal(merr)
		}
		text := string(out)
		if err != nil {
			text += err.Error()
		}
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Fatalf("%s: leaked %q in %s", name, s, text)
			}
		}
		if err := ValidateResponse(req, resp); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if hits.Load() != 2 {
			t.Fatalf("%s: hits=%d", name, hits.Load())
		}
	}
}

func TestJevSelectorEndToEnd(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	for conf, want := range map[string]string{"0.83": ReasonClassified, "0.59": ReasonLowConfidence} {
		j, hits := jevServer(t, reply(200, answerJSON(testModel, "cand-b", conf)))
		got := (Selector{Classifier: j}).Select(context.Background(), req)
		checkIdentity(t, req, got)
		if got.Reason != want || hits.Load() != 1 || *got.SelectorModel != testModel {
			t.Fatalf("%s: %#v hits=%d", conf, got, hits.Load())
		}
	}
	j, hits := jevServer(t, reply(529, `overloaded`))
	got := (Selector{Classifier: j}).Select(context.Background(), req)
	checkIdentity(t, req, got)
	if got.Status != StatusError || got.Reason != ReasonOverloaded || hits.Load() != 1 {
		t.Fatalf("%#v hits=%d", got, hits.Load())
	}
}

func TestJevBodyLimitBoundary(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	ok := answerJSON(testModel, "cand-a", "0.9")
	for size, wantErr := range map[int]bool{maxJevBodyBytes: false, maxJevBodyBytes + 1: true} {
		j, _ := jevServer(t, reply(200, ok+strings.Repeat(" ", size-len(ok))))
		_, err := j.Choose(context.Background(), req)
		if (err != nil) != wantErr {
			t.Fatalf("size %d: err=%v", size, err)
		}
		if wantErr {
			wantReason(t, err, ReasonInvalidResponse)
		}
	}
}

func TestJevAllowsUnknownProviderFields(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	body := `{"id":"r1","model":"jev-1.13.0","answers":{"candidate":{"type":"choice","choice":"cand-b","confidence":0.7,"reasoning":{"x":[1,2]}},"extra":{"type":"boolean"}},"usage":{"input_tokens":3,"output_tokens":4,"cached":1}}`
	j, _ := jevServer(t, reply(200, body))
	got, err := j.Choose(context.Background(), req)
	if err != nil || *got.CandidateID != "cand-b" || *got.Confidence != 0.7 || *got.Usage != (Usage{3, 4}) {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestJevNegativeZeroConfidence(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	for _, conf := range []string{"-0", "-0.0", "-0e0"} {
		j, _ := jevServer(t, reply(200, answerJSON(testModel, "cand-a", conf)))
		got, err := j.Choose(context.Background(), req)
		if err != nil || got.Confidence == nil || *got.Confidence != 0 || math.Signbit(*got.Confidence) {
			t.Fatalf("%s: %#v %v", conf, got, err)
		}
		out, _ := json.Marshal(got.Confidence)
		if string(out) != "0" {
			t.Fatalf("%s: encoded %s", conf, out)
		}
	}
}

func TestJevNeverFollowsRedirects(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	second, secondHits := jevServer(t, reply(200, answerJSON(testModel, "cand-a", "0.9")))
	for _, code := range []int{301, 302, 303, 307, 308} {
		for name, client := range map[string]*http.Client{"default": nil, "injected": {}, "injected_follow": {CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}} {
			j, hits := jevServer(t, func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				http.Redirect(w, r, second.URL, code)
			})
			j.Client = client
			_, err := j.Choose(context.Background(), req)
			wantReason(t, err, ReasonNetwork)
			if hits.Load() != 1 || secondHits.Load() != 0 {
				t.Fatalf("%d %s: hits=%d second=%d", code, name, hits.Load(), secondHits.Load())
			}
			if client != nil && client.CheckRedirect != nil && name == "injected" {
				t.Fatalf("%s: injected client mutated", name)
			}
		}
	}
}

type deadlineProbe struct {
	deadline time.Time
	ok       bool
	calls    int
}

func (p *deadlineProbe) RoundTrip(r *http.Request) (*http.Response, error) {
	p.calls++
	p.deadline, p.ok = r.Context().Deadline()
	body := answerJSON(testModel, "cand-a", "0.9")
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestJevDefaultDeadlineIsTwoSeconds(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	probe := &deadlineProbe{}
	j := Jev{Client: &http.Client{Transport: probe}, URL: "http://127.0.0.1:1/v1/systemone", APIKey: testKey, Model: testModel}
	before := time.Now()
	if _, err := j.Choose(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if probe.calls != 1 || !probe.ok {
		t.Fatalf("calls=%d deadline set=%v", probe.calls, probe.ok)
	}
	if probe.deadline.Before(before.Add(2*time.Second)) || probe.deadline.After(after.Add(2*time.Second)) {
		t.Fatalf("deadline %v not within [%v, %v]", probe.deadline, before.Add(2*time.Second), after.Add(2*time.Second))
	}
	if jevTimeout != 2*time.Second {
		t.Fatalf("jevTimeout = %v", jevTimeout)
	}
}

func TestJevGuardsSkipHTTP(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	j, hits := jevServer(t, reply(200, answerJSON(testModel, "cand-a", "0.9")))
	for name, model := range map[string]string{"latest": "jev-latest", "preview": "jev-preview", "unversioned": "jev", "spaces": "jev 1.13.0", "partial": "jev-1.13", "suffix": "jev-1.13.0-rc1"} {
		bad := j
		bad.Model = model
		_, err := bad.Choose(context.Background(), req)
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		wantReason(t, err, ReasonInvalidRequest)
	}
	for _, n := range []int{0, 1} {
		small := req
		small.Candidates = req.Candidates[:n]
		_, err := j.Choose(context.Background(), small)
		wantReason(t, err, ReasonInvalidRequest)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits=%d", hits.Load())
	}
}
