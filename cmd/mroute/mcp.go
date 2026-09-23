package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func serveMCP(in io.Reader, out io.Writer) error {
	return serveMCPWithRun(in, out, func(prompt string) (string, error) {
		binary, err := os.Executable()
		if err != nil {
			return "", err
		}
		// The prompt is one argv element. No shell parses its contents.
		cmd := exec.Command(binary, "run", prompt)
		output, err := cmd.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			return "", fmt.Errorf("mroute run: %s", message)
		}
		return string(output), nil
	})
}

func serveMCPWithRun(in io.Reader, out io.Writer, run func(string) (string, error)) error {
	dec, enc := json.NewDecoder(in), json.NewEncoder(out)
	for {
		var msg mcpMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(msg.ID) == 0 { // Notifications have no response.
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
		switch msg.Method {
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]string{"name": "mroute", "version": "0.1.0"},
			}
		case "ping":
			response["result"] = map[string]any{}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{map[string]any{
				"name": "route", "description": "Run a standalone task through mroute's selected CLI, model, and effort. Does not change this conversation's model.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"prompt": map[string]string{"type": "string"}}, "required": []string{"prompt"}, "additionalProperties": false},
			}}}
		case "tools/call":
			var params struct {
				Name      string `json:"name"`
				Arguments struct {
					Prompt string `json:"prompt"`
				} `json:"arguments"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil || params.Name != "route" || params.Arguments.Prompt == "" {
				response["error"] = map[string]any{"code": -32602, "message": "route requires a nonempty prompt"}
				break
			}
			output, err := run(params.Arguments.Prompt)
			if err != nil {
				response["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": err.Error()}}, "isError": true}
			} else {
				response["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": output}}}
			}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
}
