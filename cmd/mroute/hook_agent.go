package main

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/alaborderie/f-0923-jev-model-router/internal/router"
)

func hookAgent(in io.Reader, out io.Writer) error {
	return hookAgentWithRoute(in, out, routeForHook)
}

func hookAgentWithRoute(in io.Reader, out io.Writer, route func(string) (router.Entry, error)) error {
	var event struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&event); err != nil {
		return nil
	}
	if event.ToolName != "Agent" && event.ToolName != "Task" {
		return nil
	}
	if event.ToolInput == nil || event.ToolInput["model"] != nil {
		return nil
	}
	prompt, ok := event.ToolInput["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return nil
	}
	entry, err := route(prompt)
	if err != nil || entry.Outcome != "ok" || entry.Engine != "claude" {
		return nil
	}
	model := claudeModelAlias(entry.Model)
	if model == "" {
		return nil
	}
	event.ToolInput["model"] = model
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "updatedInput": event.ToolInput,
	}})
}

func claudeModelAlias(model string) string {
	for _, alias := range []string{"haiku", "sonnet", "opus"} {
		if model == alias || strings.HasPrefix(model, "claude-"+alias+"-") {
			return alias
		}
	}
	return ""
}
