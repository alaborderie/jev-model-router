package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/classifier"
	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
)

type Entry struct {
	Timestamp           time.Time `json:"timestamp"`
	QueryHash           string    `json:"query_hash"`
	TargetID            string    `json:"target_id"`
	Engine              string    `json:"engine"`
	Model               string    `json:"model"`
	Effort              string    `json:"effort"`
	ChoiceConfidence    float64   `json:"choice_confidence"`
	ScoreConfidence     float64   `json:"score_confidence"`
	ClassifierBackend   string    `json:"classifier_backend"`
	ClassifierLatencyMS int64     `json:"classifier_latency_ms"`
	Outcome             string    `json:"outcome"`
	Reason              string    `json:"reason"`
	Argv                []string  `json:"argv"`
}
type Runner struct {
	Classifier classifier.Classifier
	Config     config.Config
	LogPath    string
	Stdout     io.Writer
	Stderr     io.Writer
	Command    func(context.Context, string, ...string) *exec.Cmd
}

func ClassifierFor(c config.Config) classifier.Classifier {
	switch c.Classifier.Backend {
	case "jev":
		return classifier.Jev{}
	case "openai":
		return classifier.OpenAI{}
	default:
		return classifier.Heuristics{}
	}
}
func LogPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "mroute", "routes.jsonl"), nil
}
func Args(t config.Target, effort, prompt string) (string, []string, error) {
	switch t.Engine {
	case "claude":
		return "claude", []string{"-p", "--model", t.Model, "--effort", effort, prompt}, nil
	case "opencode":
		return "opencode", []string{"run", "-m", t.Model, "--variant", effort, prompt}, nil
	case "codex":
		return "codex", []string{"exec", "-m", t.Model, "-c", "model_reasoning_effort=" + effort, prompt}, nil
	default:
		return "", nil, fmt.Errorf("unsupported engine %q", t.Engine)
	}
}
func (r Runner) Decide(ctx context.Context, prompt string) (Entry, error) {
	c := r.Config
	cl := r.Classifier
	if cl == nil {
		cl = ClassifierFor(c)
	}
	timeout := time.Duration(c.Classifier.TimeoutMS) * time.Millisecond
	classCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	d, err := cl.Classify(classCtx, prompt, c)
	latency := time.Since(started)
	outcome := "ok"
	reason := "classified"
	if err != nil {
		outcome = "classifier_error"
		var ce *classifier.Error
		if errors.As(err, &ce) && ce.Kind == classifier.Timeout {
			outcome = "timeout"
		}
		reason = err.Error()
		d = classifier.Decision{TargetID: c.Routing.SafeTarget, Effort: "high", Backend: c.Classifier.Backend}
	}
	if err == nil && d.ChoiceConfidence < c.Classifier.ConfidenceGate {
		outcome = "low_confidence"
		reason = "choice confidence below gate"
		d.TargetID = c.Routing.SafeTarget
	}
	target, ok := c.Target(d.TargetID)
	if !ok {
		return Entry{}, fmt.Errorf("selected target %q is absent", d.TargetID)
	}
	effort := classifier.ClampEffort(d.Effort, target)
	hash := sha256.Sum256([]byte(prompt))
	entry := Entry{Timestamp: time.Now().UTC(), QueryHash: hex.EncodeToString(hash[:]), TargetID: target.ID, Engine: target.Engine, Model: target.Model, Effort: effort, ChoiceConfidence: d.ChoiceConfidence, ScoreConfidence: d.ScoreConfidence, ClassifierBackend: c.Classifier.Backend, ClassifierLatencyMS: latency.Milliseconds(), Outcome: outcome, Reason: reason}
	binary, args, e := Args(target, effort, prompt)
	if e != nil {
		return Entry{}, e
	}
	entry.Argv = append([]string{binary}, args[:len(args)-1]...)
	return entry, nil
}
func (r Runner) Route(ctx context.Context, prompt string) (Entry, error) {
	entry, err := r.Decide(ctx, prompt)
	if err != nil {
		return Entry{}, err
	}
	if err := r.Append(entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}
func (r Runner) Run(ctx context.Context, prompt string) (Entry, error) {
	entry, err := r.Route(ctx, prompt)
	if err != nil {
		return Entry{}, err
	}
	t, _ := r.Config.Target(entry.TargetID)
	bin, args, _ := Args(t, entry.Effort, prompt)
	makeCmd := r.Command
	if makeCmd == nil {
		makeCmd = exec.CommandContext
	}
	cmd := makeCmd(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if r.Stderr != nil {
		fmt.Fprintf(r.Stderr, "routed to %s @ effort %s because %s\n", entry.TargetID, entry.Effort, entry.Reason)
	}
	return entry, cmd.Run()
}
func (r Runner) Append(entry Entry) error {
	path := r.LogPath
	if path == "" {
		var err error
		path, err = LogPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}
func Read(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}
