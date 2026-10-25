package router

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"time"

	"github.com/alaborderie/jev-model-router/internal/config"
)

type Capability struct {
	Engine string
	Flags  []string
	OK     bool
	Detail string
}

// Doctor probes the configured engine CLIs without launching a model request.
func Doctor(ctx context.Context, c config.Config, command func(context.Context, string, ...string) *exec.Cmd) []Capability {
	if command == nil {
		command = exec.CommandContext
	}
	var results []Capability
	seen := map[string]bool{}
	for _, target := range c.Targets {
		engine := target.Engine
		if seen[engine] {
			continue
		}
		seen[engine] = true
		var args, flags []string
		switch engine {
		case "claude":
			args, flags = []string{"--help"}, []string{"-p", "--model", "--effort"}
		case "opencode":
			args, flags = []string{"run", "--help"}, []string{"-m", "--variant"}
		case "codex":
			args, flags = []string{"exec", "--help"}, []string{"-m", "-c"}
		}
		result := Capability{Engine: engine, Flags: flags, OK: true, Detail: "supported"}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := command(probeCtx, engine, args...).CombinedOutput()
		cancel()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() != 127 {
				result.Detail = "help probe failed"
			} else {
				result.Detail = "command unavailable"
			}
			result.OK = false
		} else {
			for _, flag := range flags {
				if !regexp.MustCompile(`(^|[\s,])` + regexp.QuoteMeta(flag) + `($|[\s,=])`).Match(output) {
					result.OK = false
					result.Detail = "missing " + flag
					break
				}
			}
		}
		results = append(results, result)
	}
	return results
}
