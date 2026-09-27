# mroute

`mroute` chooses a user-configured coding CLI, model, and effort for a prompt, then launches that CLI for a **one-shot** task. It does not change the model in an active conversation. The engine CLI keeps its own authentication; mroute does not proxy or store provider credentials.

## Install and quick start

Prerequisites: Go 1.27 or newer and at least one installed and authenticated engine CLI (`claude`, `opencode`, or `codex`).

```sh
go install github.com/alaborderie/f-0923-jev-model-router/cmd/mroute@latest
mroute init
# Review ~/.config/mroute/config.toml and set TYPESAFE_API_KEY for Jev.
mroute route "Explain this error"
mroute run "Explain this error"
```

`mroute route` prints a recommendation without launching an engine. `mroute run` launches the selected engine. `mroute log` shows the JSONL decision log and `mroute explain` shows the latest decision. Run `mroute eval eval/cases.jsonl` from the repository to measure the included 40-case set.

Use `mroute route --json "Explain this error"` for a JSON recommendation. To route prompt text beginning with a flag, put `--` before the prompt: `mroute route -- --json` routes the literal prompt `--json` in human-readable mode, and `mroute route --json -- --json` routes it in JSON mode.

## Configuration and privacy

`mroute init` creates `$XDG_CONFIG_HOME/mroute/config.toml`, or `~/.config/mroute/config.toml` when XDG_CONFIG_HOME is unset. It refuses to overwrite the file; use `mroute init --force` to replace it. Each `[[target]]` declares an engine, model, supported efforts, and a short routing description. Claude targets cannot include `none` because the Claude CLI does not accept it. `routing.safe_target` identifies the fallback when classification is uncertain or fails.

The default `jev` classifier sends a bounded prompt preview (at most `classifier.preview_bytes`, initially 8000 bytes) to TypeSafe. Set `TYPESAFE_API_KEY` in the environment, not the config. Set `classifier.backend = "heuristics"` for offline routing without a key or network. `openai` can use a local OpenAI-compatible endpoint. Full prompts and keys are not written to route logs.

The CLI runs only one task at a time. Plugin recommendations in later milestones will not silently switch the model in an existing Claude Code or opencode session.

## Claude Code plugin

After installing `mroute` and running `mroute init`, add this repository as a Claude Code marketplace and install its plugin:

```sh
claude plugin marketplace add alaborderie/f-0923-jev-model-router
claude plugin install mroute@mroute-marketplace
```

Restart Claude Code, then use `/mroute:route Explain this error` for a separate one-shot task. The plugin passes the prompt to the local `mroute` tool as structured data, which launches `mroute run`; it does not change the model of the current conversation. If the command is missing, confirm `mroute` is on Claude Code's `PATH` and that `mroute init` has created a config.

Installing and enabling the plugin also opts in to its `UserPromptSubmit` recommendation hook. The hook calls `mroute route` with a hard 1.8-second routing deadline (2 seconds at the Claude hook boundary), based on M1's 360 ms Jev p95. It adds a one-line recommendation to Claude's context; it does not switch models. If the CLI is missing, classification fails, or the deadline expires, it emits nothing and lets the prompt continue. To disable the hook, disable the plugin with `claude plugin disable mroute@mroute-marketplace`. The default Jev backend sends the bounded prompt preview described above on each prompt; select `heuristics` in the config for an offline hook.

For Claude `Agent`/legacy `Task` subagent spawns, the plugin can set the subagent model to the routed Claude family (`haiku`, `sonnet`, or `opus`). It leaves an explicitly chosen model alone and skips unsupported event shapes, non-Claude targets, and classifier failures. The subagent gets a model for its own task; the active conversation never changes models mid-turn. Its `PreToolUse` hook shares the same 1.8-second routing deadline. Both hooks are enabled with the plugin and can be turned off by disabling it.

The config at `$XDG_CONFIG_HOME/mroute/config.toml` controls all plugin paths. `classifier.backend` chooses `jev`, `openai`, or `heuristics`; `classifier.confidence_gate`, `timeout_ms`, and `preview_bytes` control classification. `routing.safe_target` is the fallback target. Each `[[target]]` supplies `id`, `engine`, `model`, `efforts`, and `when`. The TypeSafe key belongs in `TYPESAFE_API_KEY` in the environment. If you use the default Jev backend, the prompt hook sends a bounded preview of each submitted prompt to TypeSafe; `heuristics` sends nothing over the network.

## M1 routing evaluation

Run `mroute eval eval/cases.jsonl` from a repository checkout with the desired backend configured. The 40 cases are hand-labeled examples of mechanical and complex coding tasks, not a representative production sample. On 2026-09-23, the starter target catalogue produced:

| Backend | Target accuracy | Effort within band | p50 | p95 |
| --- | ---: | ---: | ---: | ---: |
| Jev (`jev-latest`) | 92.5% | 95.0% | 288 ms | 360 ms |
| Heuristics | 62.5% | 32.5% | 0 ms | 0 ms |

Jev passed the M1 gate of 80% target accuracy, 75% effort-band accuracy, 1500 ms p50, and 2500 ms p95 on this seed set. Three Jev cases fell below the configured 0.60 choice-confidence gate and routed to the safe target. Results can shift when the `jev-latest` alias or network conditions change. The confidence gate and fallback are included in the reported accuracy; `mroute eval` uses the same decision path as `route` and `run`.
