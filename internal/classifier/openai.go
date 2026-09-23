package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

type OpenAI struct{ Client *http.Client }

func (o OpenAI) Classify(ctx context.Context, prompt string, c config.Config) (Decision, error) {
	start := time.Now()
	base, err := url.Parse(c.Classifier.OpenAI.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return Decision{}, &Error{Kind: Validation, Err: errors.New("invalid OpenAI base URL")}
	}
	if len(prompt) > c.Classifier.PreviewBytes {
		prompt = prompt[:c.Classifier.PreviewBytes]
	}
	ids := make([]string, 0, len(c.Targets))
	var rubric strings.Builder
	for _, t := range c.Targets {
		ids = append(ids, t.ID)
		fmt.Fprintf(&rubric, "%s: %s\n", t.ID, t.When)
	}
	ladder, _ := Ladder(c)
	schema := map[string]any{"type": "object", "properties": map[string]any{"target_id": map[string]any{"type": "string", "enum": ids}, "effort": map[string]any{"type": "string", "enum": ladder}}, "required": []string{"target_id", "effort"}, "additionalProperties": false}
	payload := map[string]any{"model": c.Classifier.OpenAI.Model, "messages": []map[string]string{{"role": "system", "content": "Choose a configured coding target and effort. Targets:\n" + rubric.String() + "Return only the JSON schema."}, {"role": "user", "content": prompt}}, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "route", "strict": true, "schema": schema}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Decision{}, &Error{Kind: Validation, Err: err}
	}
	endpoint := strings.TrimRight(c.Classifier.OpenAI.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Decision{}, &Error{Kind: Validation, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := o.Client
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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response); err != nil || len(response.Choices) == 0 {
		return Decision{}, &Error{Kind: InvalidResponse, Err: errors.New("missing completion")}
	}
	var answer struct {
		TargetID string `json:"target_id"`
		Effort   string `json:"effort"`
	}
	decoder := json.NewDecoder(strings.NewReader(response.Choices[0].Message.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: errors.New("invalid structured output")}
	}
	d := Decision{TargetID: answer.TargetID, Effort: answer.Effort, ChoiceConfidence: 1, ScoreConfidence: 1, Latency: time.Since(start), Backend: "openai"}
	if err := ValidDecision(d, c); err != nil {
		return Decision{}, &Error{Kind: InvalidResponse, Err: err}
	}
	return d, nil
}
