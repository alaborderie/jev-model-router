package selection

import (
	"context"
	"errors"
	"slices"
	"time"
)

// ConfidenceGate is the minimum Jev confidence for a classified selection.
const ConfidenceGate = 0.60

// Error carries only a closed reason code, never provider or request content.
type Error struct{ Reason string }

func (e *Error) Error() string { return "selection: " + e.Reason }

type Classifier interface {
	Choose(context.Context, Request) (Response, error)
}

// Selector applies the protocol-v1 ConfidenceGate; the gate is not configurable.
type Selector struct {
	Classifier Classifier
}

// Select answers a validated request. It calls the classifier at most once and never falls back.
func (s Selector) Select(ctx context.Context, req Request) Response {
	resp := Response{SchemaVersion: SchemaVersion, RequestID: req.RequestID, CandidateSetHash: req.CandidateSetHash}
	switch len(req.Candidates) {
	case 0:
		resp.Status, resp.Reason = StatusAbstained, ReasonNoCandidates
		return resp
	case 1:
		id := req.Candidates[0].CandidateID
		resp.Status, resp.Reason, resp.CandidateID = StatusSelected, ReasonSingleton, &id
		return resp
	}
	if s.Classifier == nil {
		return failed(resp, ReasonInvalidRequest)
	}
	start := time.Now()
	got, err := s.Classifier.Choose(ctx, req)
	resp.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return failed(resp, errorReason(ctx, err))
	}
	if got.CandidateID == nil || !slices.ContainsFunc(req.Candidates, func(c Candidate) bool { return c.CandidateID == *got.CandidateID }) {
		return failed(resp, ReasonInvalidResponse)
	}
	resp.Confidence, resp.SelectorModel, resp.Usage = clone(got.Confidence), clone(got.SelectorModel), clone(got.Usage)
	if resp.Confidence != nil && *resp.Confidence < ConfidenceGate {
		resp.Status, resp.Reason = StatusAbstained, ReasonLowConfidence
	} else {
		resp.Status, resp.Reason, resp.CandidateID = StatusSelected, ReasonClassified, clone(got.CandidateID)
	}
	if ValidateResponse(req, resp) != nil {
		return failed(resp, ReasonInvalidResponse)
	}
	return resp
}

// errorReason maps a classifier failure to a closed reason. A typed error-status reason wins;
// an untyped failure after the context ended is cancellation or timeout, otherwise network.
func errorReason(ctx context.Context, err error) string {
	var e *Error
	switch {
	case errors.As(err, &e) && reasonRules[e.Reason].status == StatusError:
		return e.Reason
	case errors.Is(ctx.Err(), context.Canceled):
		return ReasonCancelled
	case ctx.Err() != nil:
		return ReasonTimeout
	}
	return ReasonNetwork
}

func failed(resp Response, reason string) Response {
	resp.Status, resp.Reason = StatusError, reason
	resp.CandidateID, resp.Confidence, resp.SelectorModel, resp.Usage = nil, nil, nil, nil
	return resp
}

func clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
