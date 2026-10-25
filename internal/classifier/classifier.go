package classifier

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/alaborderie/jev-model-router/internal/config"
)

type Decision struct {
	TargetID         string        `json:"target_id"`
	Effort           string        `json:"effort"`
	ChoiceConfidence float64       `json:"choice_confidence"`
	ScoreConfidence  float64       `json:"score_confidence"`
	Latency          time.Duration `json:"-"`
	Backend          string        `json:"backend"`
}
type Classifier interface {
	Classify(context.Context, string, config.Config) (Decision, error)
}
type ErrorKind string

const (
	Authentication  ErrorKind = "authentication"
	Validation      ErrorKind = "validation"
	RateLimit       ErrorKind = "rate_limit"
	Overloaded      ErrorKind = "overloaded"
	Timeout         ErrorKind = "timeout"
	Network         ErrorKind = "network"
	InvalidResponse ErrorKind = "invalid_response"
)

type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("classifier %s: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("classifier %s", e.Kind)
}
func (e *Error) Unwrap() error { return e.Err }

var effortOrder = []string{"none", "low", "medium", "high", "xhigh", "max"}
var effortDescriptions = map[string]string{"none": "Trivial lookup or direct answer; no extended reasoning", "low": "Mechanical edit or straightforward single-step task", "medium": "Several clear steps or ordinary coding task", "high": "Debugging, complex implementation or careful tradeoffs", "xhigh": "Difficult cross-file reasoning or ambiguous design", "max": "Highest complexity, extensive investigation and planning"}

func Ladder(c config.Config) ([]string, []string) {
	seen := map[string]bool{}
	for _, t := range c.Targets {
		for _, e := range t.Efforts {
			seen[e] = true
		}
	}
	var names, desc []string
	for _, e := range effortOrder {
		if seen[e] {
			names = append(names, e)
			desc = append(desc, effortDescriptions[e])
		}
	}
	return names, desc
}
func ScoreEffort(score float64, ladder []string) (string, error) {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > float64(len(ladder)-1) {
		return "", fmt.Errorf("score outside effort ladder")
	}
	return ladder[int(math.Round(score))], nil
}
func ValidDecision(d Decision, c config.Config) error {
	if _, ok := c.Target(d.TargetID); !ok {
		return fmt.Errorf("unknown target %q", d.TargetID)
	}
	valid := false
	for _, e := range effortOrder {
		if e == d.Effort {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown effort %q", d.Effort)
	}
	if d.ChoiceConfidence < 0 || d.ChoiceConfidence > 1 || d.ScoreConfidence < 0 || d.ScoreConfidence > 1 {
		return fmt.Errorf("confidence outside 0..1")
	}
	return nil
}
func ClampEffort(e string, t config.Target) string {
	idx := func(v string) int {
		for i, x := range effortOrder {
			if x == v {
				return i
			}
		}
		return 0
	}
	desired := idx(e)
	sorted := append([]string(nil), t.Efforts...)
	sort.Slice(sorted, func(i, j int) bool { return idx(sorted[i]) < idx(sorted[j]) })
	best := sorted[0]
	dist := 100
	for _, x := range sorted {
		d := idx(x) - desired
		if d < 0 {
			d = -d
		}
		if d < dist {
			best = x
			dist = d
		}
	}
	return best
}
