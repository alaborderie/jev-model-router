package eval

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/alaborderie/jev-model-router/internal/classifier"
	"github.com/alaborderie/jev-model-router/internal/config"
	"github.com/alaborderie/jev-model-router/internal/router"
)

type fixed struct{}

func (fixed) Classify(context.Context, string, config.Config) (classifier.Decision, error) {
	return classifier.Decision{TargetID: "small", Effort: "low", ChoiceConfidence: 1, ScoreConfidence: 1}, nil
}
func TestCasesAndMetrics(t *testing.T) {
	cases, err := Load(filepath.Join("..", "..", "eval", "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 40 {
		t.Fatalf("want 40 labeled cases, got %d", len(cases))
	}
	c := config.Config{Classifier: config.Classifier{Backend: "heuristics", TimeoutMS: 1000}, Routing: config.Routing{SafeTarget: "big"}, Targets: []config.Target{{ID: "small", Engine: "claude", Model: "small", Efforts: []string{"low"}}, {ID: "big", Engine: "claude", Model: "big", Efforts: []string{"high"}}}}
	set := []Case{{Prompt: "a", ExpectedTarget: "small", ExpectedEffortBand: []string{"low", "medium"}}, {Prompt: "b", ExpectedTarget: "big", ExpectedEffortBand: []string{"high", "max"}}}
	report, err := Run(context.Background(), router.Runner{Config: c, Classifier: fixed{}}, set)
	if err != nil {
		t.Fatal(err)
	}
	if report.TargetAccuracy != 0.5 || report.EffortWithinBand != 0.5 || report.Confusion["big"]["small"] != 1 {
		t.Fatalf("bad report %+v", report)
	}
}

func TestWriteTableIncludesConfiguredBackendAndModel(t *testing.T) {
	report := Report{Backend: "jev", Model: "jev-latest", Cases: 40, TargetAccuracy: 0.925, EffortWithinBand: 0.95}
	var out bytes.Buffer
	if err := WriteTable(&out, report); err != nil {
		t.Fatal(err)
	}
	want := "| Backend | Model | Cases | Target accuracy | Effort within band |\n| --- | --- | ---: | ---: | ---: |\n| jev | jev-latest | 40 | 92.5% | 95.0% |\n"
	if out.String() != want {
		t.Fatalf("table = %q, want %q", out.String(), want)
	}
}
