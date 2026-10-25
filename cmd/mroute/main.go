package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/alaborderie/jev-model-router/internal/config"
	"github.com/alaborderie/jev-model-router/internal/eval"
	"github.com/alaborderie/jev-model-router/internal/router"
)

// exitStatus ends the process with a specific code. The command that returns it has
// already written its own diagnostic, so main prints nothing further.
type exitStatus int

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(s)) }
func (s exitStatus) ExitCode() int { return int(s) }

func main() {
	if err := run(os.Args[1:]); err != nil {
		var status exitStatus
		if errors.As(err, &status) {
			os.Exit(status.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "mroute:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mroute init|route|run|log|explain|eval|doctor|serve")
	}
	if args[0] == "select" {
		// Selection never touches HOME, XDG config, the route log or engines.
		return selectCommand(args[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	if args[0] == "init" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--force") {
			return fmt.Errorf("usage: mroute init [--force]")
		}
		path, err := config.Path()
		if err != nil {
			return err
		}
		if err := config.Init(path, len(args) == 2); err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	if args[0] == "serve" {
		if len(args) != 1 {
			return fmt.Errorf("usage: mroute serve")
		}
		return http.ListenAndServe(":8080", healthHandler())
	}
	if args[0] == "mcp" {
		if len(args) != 1 {
			return fmt.Errorf("usage: mroute mcp")
		}
		return serveMCP(os.Stdin, os.Stdout)
	}
	if args[0] == "hook-prompt" {
		if len(args) != 1 {
			return fmt.Errorf("usage: mroute hook-prompt")
		}
		return hookPrompt(os.Stdin, os.Stdout)
	}
	if args[0] == "hook-agent" {
		if len(args) != 1 {
			return fmt.Errorf("usage: mroute hook-agent")
		}
		return hookAgent(os.Stdin, os.Stdout)
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	casePath := ""
	table := false
	if args[0] == "eval" {
		var configPath string
		casePath, configPath, table, err = evalArgs(args[1:])
		if err != nil {
			return err
		}
		if configPath != "" {
			path = configPath
		}
	}
	if args[0] == "doctor" && len(args) == 3 && args[1] == "--config" {
		path = args[2]
	}
	c, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logPath, err := router.LogPath()
	if err != nil {
		return err
	}
	r := router.Runner{Config: c, LogPath: logPath, Stdout: os.Stdout, Stderr: os.Stderr}
	switch args[0] {
	case "doctor":
		if len(args) != 1 && !(len(args) == 3 && args[1] == "--config") {
			return fmt.Errorf("usage: mroute doctor [--config FILE]")
		}
		failed := false
		for _, capability := range router.Doctor(context.Background(), c, nil) {
			fmt.Printf("%s: %s (required flags: %s)\n", capability.Engine, capability.Detail, strings.Join(capability.Flags, ", "))
			failed = failed || !capability.OK
		}
		if failed {
			return fmt.Errorf("one or more configured engine CLIs lack required capabilities")
		}
		return nil
	case "route", "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: mroute %s PROMPT", args[0])
		}
		var prompt string
		var jsonRoute bool
		if args[0] == "route" {
			prompt, jsonRoute, err = routePromptArgs(args[1:])
			if err != nil {
				return err
			}
		} else {
			prompt = strings.Join(args[1:], " ")
		}
		if args[0] == "route" {
			entry, err := r.Route(context.Background(), prompt)
			if err != nil {
				return err
			}
			if jsonRoute {
				return json.NewEncoder(os.Stdout).Encode(entry)
			}
			fmt.Printf("routed to %s @ effort %s because %s\n", entry.TargetID, entry.Effort, entry.Reason)
			return nil
		}
		_, err := r.Run(context.Background(), prompt)
		return err
	case "log":
		if len(args) != 1 {
			return fmt.Errorf("usage: mroute log")
		}
		entries, err := router.Read(logPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			b, _ := json.Marshal(e)
			fmt.Println(string(b))
		}
		return nil
	case "explain":
		if len(args) > 2 {
			return fmt.Errorf("usage: mroute explain [QUERY_HASH_PREFIX]")
		}
		entries, err := router.Read(logPath)
		if err != nil {
			return err
		}
		for i := len(entries) - 1; i >= 0; i-- {
			if len(args) == 1 || strings.HasPrefix(entries[i].QueryHash, args[1]) {
				b, _ := json.MarshalIndent(entries[i], "", "  ")
				fmt.Println(string(b))
				return nil
			}
		}
		return fmt.Errorf("no matching route")
	case "eval":
		cases, err := eval.Load(casePath)
		if err != nil {
			return err
		}
		report, err := eval.Run(context.Background(), r, cases)
		if err != nil {
			return err
		}
		if table {
			return eval.WriteTable(os.Stdout, report)
		}
		return eval.Write(os.Stdout, report)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func evalArgs(args []string) (casePath, configPath string, table bool, err error) {
	casePath = "eval/cases.jsonl"
	for len(args) > 0 {
		switch args[0] {
		case "--table":
			table = true
			args = args[1:]
		case "--config":
			if len(args) < 2 {
				return "", "", false, fmt.Errorf("usage: mroute eval [--table] [--config FILE] [CASES_JSONL]")
			}
			configPath, args = args[1], args[2:]
		default:
			if args[0] == "" || strings.HasPrefix(args[0], "-") || len(args) != 1 {
				return "", "", false, fmt.Errorf("usage: mroute eval [--table] [--config FILE] [CASES_JSONL]")
			}
			casePath, args = args[0], nil
		}
	}
	return casePath, configPath, table, nil
}

// routePromptArgs parses the --json output flag and treats -- as the prompt boundary.
func routePromptArgs(args []string) (string, bool, error) {
	jsonRoute := len(args) > 0 && args[0] == "--json"
	if jsonRoute {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return "", false, fmt.Errorf("usage: mroute route [--json] [--] PROMPT")
	}
	return strings.Join(args, " "), jsonRoute, nil
}
