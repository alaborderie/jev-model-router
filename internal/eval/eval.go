package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
)

type Case struct {
	Prompt             string   `json:"prompt"`
	ExpectedTarget     string   `json:"expected_target"`
	ExpectedEffortBand []string `json:"expected_effort_band"`
	Tags               []string `json:"tags"`
}
type Report struct {
	Backend          string                    `json:"backend"`
	Model            string                    `json:"model"`
	Cases            int                       `json:"cases"`
	TargetAccuracy   float64                   `json:"target_accuracy"`
	EffortWithinBand float64                   `json:"effort_within_band"`
	P50LatencyMS     int64                     `json:"p50_latency_ms"`
	P95LatencyMS     int64                     `json:"p95_latency_ms"`
	Confusion        map[string]map[string]int `json:"confusion"`
	Outcomes         map[string]int            `json:"outcomes"`
	GeneratedAt      time.Time                 `json:"generated_at"`
}

func Load(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	var cases []Case
	for scan.Scan() {
		var c Case
		if err := json.Unmarshal(scan.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("case %d: %w", len(cases)+1, err)
		}
		if c.Prompt == "" || c.ExpectedTarget == "" || len(c.ExpectedEffortBand) != 2 {
			return nil, fmt.Errorf("case %d: missing prompt, target, or effort band", len(cases)+1)
		}
		cases = append(cases, c)
	}
	return cases, scan.Err()
}
func Run(ctx context.Context, r router.Runner, cases []Case) (Report, error) {
	report := Report{Backend: r.Config.Classifier.Backend, Cases: len(cases), Confusion: map[string]map[string]int{}, Outcomes: map[string]int{}, GeneratedAt: time.Now().UTC()}
	switch report.Backend {
	case "jev":
		report.Model = r.Config.Classifier.Jev.Model
	case "openai":
		report.Model = r.Config.Classifier.OpenAI.Model
	default:
		report.Model = "built-in"
	}
	if len(cases) == 0 {
		return report, fmt.Errorf("eval set is empty")
	}
	var targetOK, effortOK int
	latencies := make([]int64, 0, len(cases))
	order := map[string]int{"none": 0, "low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 5}
	for _, c := range cases {
		entry, err := r.Decide(ctx, c.Prompt)
		if err != nil {
			return report, err
		}
		if entry.TargetID == c.ExpectedTarget {
			targetOK++
		}
		low, ok1 := order[c.ExpectedEffortBand[0]]
		high, ok2 := order[c.ExpectedEffortBand[1]]
		if !ok1 || !ok2 || low > high {
			return report, fmt.Errorf("invalid effort band for %q", c.Prompt)
		}
		if n := order[entry.Effort]; n >= low && n <= high {
			effortOK++
		}
		if report.Confusion[c.ExpectedTarget] == nil {
			report.Confusion[c.ExpectedTarget] = map[string]int{}
		}
		report.Confusion[c.ExpectedTarget][entry.TargetID]++
		report.Outcomes[entry.Outcome]++
		latencies = append(latencies, entry.ClassifierLatencyMS)
	}
	report.TargetAccuracy = float64(targetOK) / float64(len(cases))
	report.EffortWithinBand = float64(effortOK) / float64(len(cases))
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report.P50LatencyMS = latencies[(len(latencies)-1)*50/100]
	report.P95LatencyMS = latencies[(len(latencies)-1)*95/100]
	return report, nil
}
func Write(w io.Writer, r Report) error {
	_, err := fmt.Fprintf(w, "Backend: %s\nModel: %s\nCases: %d\nTarget accuracy: %.1f%%\nEffort within band: %.1f%%\nClassifier p50: %d ms\nClassifier p95: %d ms\nOutcomes: %v\n", r.Backend, r.Model, r.Cases, 100*r.TargetAccuracy, 100*r.EffortWithinBand, r.P50LatencyMS, r.P95LatencyMS, r.Outcomes)
	return err
}

func WriteTable(w io.Writer, r Report) error {
	_, err := fmt.Fprintf(w, "| Backend | Model | Cases | Target accuracy | Effort within band |\n| --- | --- | ---: | ---: | ---: |\n| %s | %s | %d | %.1f%% | %.1f%% |\n", r.Backend, r.Model, r.Cases, 100*r.TargetAccuracy, 100*r.EffortWithinBand)
	return err
}
