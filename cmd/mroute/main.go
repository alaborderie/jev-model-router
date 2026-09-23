package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
	"github.com/alaborderie/f-0923-jev-model-router/internal/eval"
	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mroute:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mroute init|route|run|log|explain|eval")
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
		jsonRoute := args[0] == "route" && args[1] == "--json"
		if jsonRoute && len(args) < 3 {
			return fmt.Errorf("usage: mroute route --json PROMPT")
		}
		promptArgs := args[1:]
		if jsonRoute {
			promptArgs = args[2:]
		}
		prompt := strings.Join(promptArgs, " ")
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
