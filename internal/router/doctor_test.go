package router

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/alaborderie/jev-model-router/internal/config"
)

func TestDoctorReportsUnsupportedFlagsAndMissingEngine(t *testing.T) {
	t.Setenv("GO_WANT_DOCTOR_HELPER", "1")
	c := config.Config{Targets: []config.Target{{Engine: "claude"}, {Engine: "opencode"}, {Engine: "codex"}}}
	probe := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestDoctorHelper", "--", name, strings.Join(args, " "))
	}
	results := Doctor(context.Background(), c, probe)
	if len(results) != 3 || !results[0].OK || results[1].OK || results[1].Detail != "missing --variant" || results[2].OK || results[2].Detail != "command unavailable" {
		t.Fatalf("doctor results: %+v", results)
	}
}

func TestDoctorDoesNotMistakeLongFlagForShortFlag(t *testing.T) {
	t.Setenv("GO_WANT_DOCTOR_HELPER", "1")
	c := config.Config{Targets: []config.Target{{Engine: "opencode"}}}
	probe := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestDoctorHelper", "--", "opencode-long-only")
	}
	results := Doctor(context.Background(), c, probe)
	if len(results) != 1 || results[0].OK || results[0].Detail != "missing -m" {
		t.Fatalf("doctor results: %+v", results)
	}
}

func TestDoctorHelper(t *testing.T) {
	if os.Getenv("GO_WANT_DOCTOR_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" || i+1 >= len(os.Args) {
			continue
		}
		switch os.Args[i+1] {
		case "claude":
			os.Stdout.WriteString("-p --model --effort")
		case "opencode":
			os.Stdout.WriteString("-m --model")
		case "opencode-long-only":
			os.Stdout.WriteString("--model --variant")
		default:
			os.Exit(127)
		}
		os.Exit(0)
	}
}
