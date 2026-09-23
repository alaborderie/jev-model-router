package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
)

// The Jev seed-set p95 was 360 ms. Leave room for process startup and network
// variance while keeping the hook below the architecture's 2.5-second ceiling.
const promptHookBudget = 1800 * time.Millisecond

func hookPrompt(in io.Reader, out io.Writer) error {
	return hookPromptWithRoute(in, out, func(prompt string) (router.Entry, error) {
		binary, err := os.Executable()
		if err != nil {
			return router.Entry{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), promptHookBudget)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "route", "--json", prompt)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			return router.Entry{}, err
		}
		var entry router.Entry
		if err := json.Unmarshal(stdout.Bytes(), &entry); err != nil {
			return router.Entry{}, err
		}
		return entry, nil
	})
}

func hookPromptWithRoute(in io.Reader, out io.Writer, route func(string) (router.Entry, error)) error {
	var event struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&event); err != nil || strings.TrimSpace(event.Prompt) == "" {
		return nil
	}
	entry, err := route(event.Prompt)
	if err != nil || entry.Outcome != "ok" {
		return nil
	}
	context := fmt.Sprintf("mroute recommends %s at %s effort for a separate one-shot task. This does not change the active conversation's model; use /mroute:route to delegate it.", entry.TargetID, entry.Effort)
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "UserPromptSubmit", "additionalContext": context}})
}
