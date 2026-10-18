package selection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

type countingClassifier struct{ calls int }

func (f *countingClassifier) Choose(context.Context, Request) (Response, error) {
	f.calls++
	return Response{}, errors.New("should not be called")
}

type fakeClassifier struct {
	resp  Response
	err   error
	calls int
}

func (f *fakeClassifier) Choose(context.Context, Request) (Response, error) {
	f.calls++
	return f.resp, f.err
}

func inference(id string, conf float64) Response {
	return Response{CandidateID: &id, Confidence: &conf, SelectorModel: ptr("jev-1.13.0"), Usage: &Usage{InputTokens: 512, OutputTokens: 1}}
}

func checkIdentity(t *testing.T, req Request, got Response) {
	t.Helper()
	if got.SchemaVersion != 1 || got.RequestID != req.RequestID || got.CandidateSetHash != req.CandidateSetHash {
		t.Fatalf("identity not echoed: %#v", got)
	}
	if err := ValidateResponse(req, got); err != nil {
		t.Fatalf("response violates contract: %v %#v", err, got)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("response not encodable: %v", err)
	}
}

func wantNulls(t *testing.T, got Response) {
	t.Helper()
	if got.CandidateID != nil || got.Confidence != nil || got.SelectorModel != nil || got.Usage != nil {
		t.Fatalf("want null inference fields: %#v", got)
	}
}

func TestEmptySkipsClassifier(t *testing.T) {
	f := &countingClassifier{}
	got := (Selector{Classifier: f}).Select(context.Background(), Request{})
	if got.Status != "abstained" || got.Reason != "no_candidates" || f.calls != 0 {
		t.Fatalf("unexpected empty-set outcome: %#v calls=%d", got, f.calls)
	}
	req := decodeFixture(t, "empty.json")
	got = (Selector{Classifier: f}).Select(context.Background(), req)
	checkIdentity(t, req, got)
	wantNulls(t, got)
	if got.Status != StatusAbstained || got.Reason != ReasonNoCandidates || got.LatencyMS != 0 || f.calls != 0 {
		t.Fatalf("unexpected empty-set outcome: %#v calls=%d", got, f.calls)
	}
}

func TestSingletonSkipsClassifier(t *testing.T) {
	req := decodeFixture(t, "singleton.json")
	for _, c := range []Classifier{&countingClassifier{}, nil} {
		got := (Selector{Classifier: c}).Select(context.Background(), req)
		checkIdentity(t, req, got)
		if got.Status != StatusSelected || got.Reason != ReasonSingleton || got.CandidateID == nil || *got.CandidateID != req.Candidates[0].CandidateID {
			t.Fatalf("unexpected singleton outcome: %#v", got)
		}
		if got.Confidence != nil || got.SelectorModel != nil || got.Usage != nil || got.LatencyMS != 0 {
			t.Fatalf("singleton carries inference: %#v", got)
		}
		if f, ok := c.(*countingClassifier); ok && f.calls != 0 {
			t.Fatalf("singleton called classifier %d times", f.calls)
		}
	}
}

func TestSelectCallsClassifierExactlyOnce(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	f := &countingClassifier{}
	got := (Selector{Classifier: f}).Select(context.Background(), req)
	checkIdentity(t, req, got)
	wantNulls(t, got)
	if f.calls != 1 || got.Status != StatusError || got.Reason != ReasonNetwork {
		t.Fatalf("want one call and network error, got %#v calls=%d", got, f.calls)
	}
}

func TestSelectNilClassifierIsInvalidRequest(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	got := (Selector{}).Select(context.Background(), req)
	checkIdentity(t, req, got)
	wantNulls(t, got)
	if got.Status != StatusError || got.Reason != ReasonInvalidRequest {
		t.Fatalf("unexpected outcome: %#v", got)
	}
}

func TestConfidenceGate(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	for _, tc := range []struct {
		conf           float64
		status, reason string
	}{{0.59, StatusAbstained, ReasonLowConfidence}, {0.5999, StatusAbstained, ReasonLowConfidence}, {0, StatusAbstained, ReasonLowConfidence}, {0.60, StatusSelected, ReasonClassified}, {1, StatusSelected, ReasonClassified}} {
		f := &fakeClassifier{resp: inference("cand-b", tc.conf)}
		got := (Selector{Classifier: f}).Select(context.Background(), req)
		checkIdentity(t, req, got)
		if f.calls != 1 || got.Status != tc.status || got.Reason != tc.reason {
			t.Fatalf("conf %v: %#v calls=%d", tc.conf, got, f.calls)
		}
		if got.Confidence == nil || *got.Confidence != tc.conf || got.SelectorModel == nil || *got.SelectorModel != "jev-1.13.0" || got.Usage == nil || *got.Usage != (Usage{512, 1}) {
			t.Fatalf("conf %v: inference fields lost: %#v", tc.conf, got)
		}
		if tc.status == StatusSelected && *got.CandidateID != "cand-b" || tc.status == StatusAbstained && got.CandidateID != nil {
			t.Fatalf("conf %v: candidate_id %v", tc.conf, got.CandidateID)
		}
	}
	if n := reflect.TypeOf(Selector{}).NumField(); n != 1 || ConfidenceGate != 0.60 {
		t.Fatalf("the 0.60 gate is a protocol constant, not configuration: %d fields", n)
	}
}

func TestSelectRejectsInvalidClassifierOutput(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	noModel := inference("cand-a", 0.9)
	noModel.SelectorModel = nil
	noConf := inference("cand-a", 0.9)
	noConf.Confidence = nil
	noID := inference("cand-a", 0.9)
	noID.CandidateID = nil
	badModel := inference("cand-a", 0.9)
	badModel.SelectorModel = ptr("bad model")
	negUsage := inference("cand-a", 0.9)
	negUsage.Usage = &Usage{-1, 1}
	for name, resp := range map[string]Response{
		"out_of_set": inference("cand-z", 0.9), "nan": inference("cand-a", math.NaN()), "inf": inference("cand-a", math.Inf(1)),
		"over_one": inference("cand-a", 1.5), "negative": inference("cand-a", -0.1), "low_nan": inference("cand-z", math.NaN()), "low_out_of_set": inference("cand-z", 0.1),
		"no_model": noModel, "no_confidence": noConf, "no_id": noID, "bad_model": badModel, "negative_usage": negUsage,
	} {
		f := &fakeClassifier{resp: resp}
		got := (Selector{Classifier: f}).Select(context.Background(), req)
		checkIdentity(t, req, got)
		wantNulls(t, got)
		if f.calls != 1 || got.Status != StatusError || got.Reason != ReasonInvalidResponse {
			t.Fatalf("%s: %#v calls=%d", name, got, f.calls)
		}
	}
}

func TestSelectErrorReasons(t *testing.T) {
	req := decodeFixture(t, "unicode.json")
	cases := map[string]error{
		ReasonNetwork:         errors.New("dial tcp 10.0.0.1: secret"),
		ReasonTimeout:         fmt.Errorf("wrapped: %w", &Error{Reason: ReasonTimeout}),
		ReasonCancelled:       &Error{Reason: ReasonCancelled},
		ReasonAuthentication:  &Error{Reason: ReasonAuthentication},
		ReasonRateLimit:       &Error{Reason: ReasonRateLimit},
		ReasonOverloaded:      &Error{Reason: ReasonOverloaded},
		ReasonInvalidResponse: &Error{Reason: ReasonInvalidResponse},
		ReasonInvalidRequest:  &Error{Reason: ReasonInvalidRequest},
	}
	for want, err := range cases {
		f := &fakeClassifier{resp: inference("cand-a", 0.9), err: err}
		got := (Selector{Classifier: f}).Select(context.Background(), req)
		checkIdentity(t, req, got)
		wantNulls(t, got)
		if f.calls != 1 || got.Status != StatusError || got.Reason != want {
			t.Fatalf("%s: %#v calls=%d", want, got, f.calls)
		}
	}
	got := (Selector{Classifier: &fakeClassifier{err: &Error{Reason: ReasonSingleton}}}).Select(context.Background(), req)
	if got.Status != StatusError || got.Reason != ReasonNetwork {
		t.Fatalf("non-error reason must not leak through: %#v", got)
	}
}

func TestErrorTextIsReasonOnly(t *testing.T) {
	if got := (&Error{Reason: ReasonRateLimit}).Error(); got != "selection: rate_limit" {
		t.Fatalf("got %q", got)
	}
}
