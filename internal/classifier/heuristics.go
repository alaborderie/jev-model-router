package classifier

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

type Heuristics struct{}

func tokens(s string) map[string]bool {
	result := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(word) > 2 {
			result[word] = true
		}
	}
	return result
}
func (Heuristics) Classify(ctx context.Context, prompt string, c config.Config) (Decision, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		if err == context.Canceled {
			return Decision{}, err
		}
		return Decision{}, &Error{Kind: Timeout, Err: err}
	}
	words := tokens(prompt)
	best := c.Routing.SafeTarget
	top := 0
	for _, t := range c.Targets {
		n := 0
		for w := range tokens(t.When) {
			if words[w] {
				n++
			}
		}
		if n > top {
			top = n
			best = t.ID
		}
	}
	target, _ := c.Target(best)
	effort := "medium"
	for _, w := range []string{"architecture", "debug", "investigate", "refactor", "complex", "ambiguous"} {
		if words[w] {
			effort = "high"
			break
		}
	}
	if effort == "medium" {
		for _, w := range []string{"rename", "format", "typo", "lookup", "simple", "mechanical"} {
			if words[w] {
				effort = "low"
				break
			}
		}
	}
	return Decision{TargetID: best, Effort: ClampEffort(effort, target), ChoiceConfidence: 1, ScoreConfidence: 1, Latency: time.Since(start), Backend: "heuristics"}, nil
}
