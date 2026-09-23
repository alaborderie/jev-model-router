package main

import (
	"fmt"
	"github.com/alaborderie/f-0923-jev-model-router/internal/config"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mroute:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mroute init [--force]")
	}
	switch args[0] {
	case "init":
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
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
