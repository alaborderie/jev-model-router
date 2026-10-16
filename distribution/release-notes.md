Local model-and-effort routing for separate coding tasks in Claude Code, opencode and Codex.

- Inspect decisions with route, explain and log before running a task.
- First initialization selects offline heuristics without TYPESAFE_API_KEY; Jev is selected when a key is present.
- Claude and opencode integrations recommend or delegate; they do not switch the active conversation model.
- Linux/macOS archives for amd64/arm64, MIT license, SHA256 checksums (not signatures).

Requirements: configure supported targets and install/authenticate the corresponding engine CLI.
Heuristics classification is offline. Jev sends a bounded prompt preview to TypeSafe;
engine execution uses that engine's own authentication and data policy.

The included 40 hand-labeled cases are a small seed set, not production accuracy evidence.
See eval/results.md and README for the reproducible offline baseline and separately
labelled historical Jev measurements. No savings or latency guarantee.
