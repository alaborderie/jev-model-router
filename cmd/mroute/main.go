package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
	"github.com/alaborderie/f-0923-jev-model-router/internal/eval"
	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
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
		return fmt.Errorf("usage: mroute init|route|run|log|explain|eval")
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
	path, err := config.Path()
	if err != nil {
		return err
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
	case "route", "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: mroute %s PROMPT", args[0])
		}
		prompt := strings.Join(args[1:], " ")
		if args[0] == "route" {
			entry, err := r.Route(context.Background(), prompt)
			if err != nil {
				return err
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
		if len(args) > 2 {
			return fmt.Errorf("usage: mroute eval [CASES_JSONL]")
		}
		casePath := "eval/cases.jsonl"
		if len(args) == 2 {
			casePath = args[1]
		}
		cases, err := eval.Load(casePath)
		if err != nil {
			return err
		}
		report, err := eval.Run(context.Background(), r, cases)
		if err != nil {
			return err
		}
		return eval.Write(os.Stdout, report)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
