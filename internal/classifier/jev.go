package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

const JevURL = "https://api.typesafe.ai/v1/systemone"

type Jev struct {
	URL    string
	Client *http.Client
}
type question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria"`
}

func (j Jev) Classify(ctx context.Context, prompt string, c config.Config) (Decision, error) {
	start := time.Now()
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		return Decision{}, &Error{Kind: Authentication, Err: errors.New("set TYPESAFE_API_KEY")}
	}
	ladder, descriptions := Ladder(c)
	if len(ladder) < 2 || len(ladder) > 10 {
		return Decision{}, &Error{Kind: Validation, Err: errors.New("effort ladder must contain 2..10 levels")}
	}
	criteria := map[string]string{}
	for _, t := range c.Targets {
		criteria[t.ID] = t.When
	}
	if len(prompt) > c.Classifier.PreviewBytes {
		prompt = prompt[:c.Classifier.PreviewBytes]
	}
	payload := struct {
		State     string              `json:"state"`
		Model     string              `json:"model"`
		Questions map[string]question `json:"questions"`
	}{prompt, c.Classifier.Jev.Model, map[string]question{"target": {Type: "choice", Instructions: "Which configured coding target best fits this task?", Criteria: criteria}, "effort": {Type: "score", Instructions: "How much reasoning effort does this coding task require?", Criteria: descriptions}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Decision{}, &Error{Kind: Validation, Err: err}
	}
	url := j.URL
	if url == "" {
		url = JevURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Decision{}, &Error{Kind: Validation, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := j.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Decision{}, &Error{Kind: Timeout, Err: errors.New("request timed out")}
		}
		return Decision{}, &Error{Kind: Network, Err: errors.New("request failed")}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		kind := Network
		switch res.StatusCode {
		case 401:
			kind = Authentication
		case 422:
			kind = Validation
		case 429:
			kind = RateLimit
		case 529:
			kind = Overloaded
		}
		return Decision{}, &Error{Kind: kind, Err: fmt.Errorf("HTTP %d", res.StatusCode)}
	}
	var response struct {
		Answers struct {
			Target struct {
				Type       string   `json:"type"`
				Choice     string   `json:"choice"`
				Confidence *float64 `json:"confidence"`
			} `json:"target"`
			Effort struct {
				Type       string   `json:"type"`
				Score      *float64 `json:"score"`
				Confidence *float64 `json:"confidence"`
			} `json:"effort"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: errors.New("malformed JSON")}
	}
	a, b := response.Answers.Target, response.Answers.Effort
	if a.Type != "choice" || b.Type != "score" || a.Confidence == nil || b.Confidence == nil || b.Score == nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: errors.New("missing typed answers")}
	}
	effort, err := ScoreEffort(*b.Score, ladder)
	if err != nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: err}
	}
	d := Decision{TargetID: a.Choice, Effort: effort, ChoiceConfidence: *a.Confidence, ScoreConfidence: *b.Confidence, Latency: time.Since(start), Backend: "jev"}
	if err := ValidDecision(d, c); err != nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: err}
	}
	return d, nil
}
