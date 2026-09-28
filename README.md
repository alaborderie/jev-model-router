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

## opencode plugin

Install `mroute` and initialize its config as above. Copy `plugin/opencode/mroute.js` to `~/.config/opencode/plugins/mroute.js` and `plugin/opencode/lib/route-core.mjs` to `~/.config/opencode/plugins/lib/route-core.mjs` (create the `plugins/lib` directory first), then restart opencode. Local plugins use opencode's `@opencode-ai/plugin` helper.

Ask the agent to use the `route` tool with a prompt to launch a **separate one-shot task** via `mroute run`; its output is returned to the agent. The `recommend` tool runs `mroute route --json`, shows a toast and appends the recommendation to the TUI prompt when those surfaces are available. The recommendation also appears in the tool result if the TUI is unavailable. Neither tool switches the model of the active conversation. The configured Jev backend sends the bounded preview described under Configuration and privacy; use `heuristics` for offline routing. Run `node --test plugin/opencode/route.test.mjs` to exercise the local tool fixture.

## M1 routing evaluation

Run `mroute eval eval/cases.jsonl` from a repository checkout with the desired backend configured. The 40 cases are hand-labeled examples of mechanical and complex coding tasks, not a representative production sample. On 2026-09-23, the starter target catalogue produced:

For a deterministic offline table, run `go run ./cmd/mroute eval --table --config eval/heuristics.toml eval/cases.jsonl`; the generated output is committed at [eval/results.md](eval/results.md). The Jev figures below are a historical live run, not a deterministic replay: the `jev-latest` alias and latency may change.

| Backend | Target accuracy | Effort within band | p50 | p95 |
| --- | ---: | ---: | ---: | ---: |
| Jev (`jev-latest`) | 92.5% | 95.0% | 288 ms | 360 ms |
| Heuristics | 62.5% | 32.5% | 0 ms | 0 ms |

Jev passed the M1 gate of 80% target accuracy, 75% effort-band accuracy, 1500 ms p50, and 2500 ms p95 on this seed set. Three Jev cases fell below the configured 0.60 choice-confidence gate and routed to the safe target. Results can shift when the `jev-latest` alias or network conditions change. The confidence gate and fallback are included in the reported accuracy; `mroute eval` uses the same decision path as `route` and `run`.

## Configuration reference

`mroute init` writes the starter values below; a custom TOML file must supply the required fields (there are no implicit config defaults). `mroute eval --config FILE` and `mroute doctor --config FILE` let you inspect a particular catalogue without changing your personal config.

| Field | Starter value | Meaning |
| --- | --- | --- |
| `classifier.backend` | `jev` | `jev`, `openai`, or offline `heuristics` |
| `classifier.confidence_gate` | `0.60` | Choice confidence below this 0–1 threshold routes to the safe target |
| `classifier.timeout_ms` | `2000` | Positive classifier deadline in milliseconds |
| `classifier.preview_bytes` | `8000` | Maximum prompt preview bytes sent to a classifier (1–32768) |
| `classifier.jev.model` | `jev-latest` | TypeSafe model; key comes from `TYPESAFE_API_KEY` in the environment |
| `classifier.openai.base_url` | `http://127.0.0.1:8080/v1` | OpenAI-compatible endpoint (may be local) |
| `classifier.openai.model` | `local` | Model served by that endpoint |
| `routing.safe_target` | `claude-opus` | ID of the target used for low confidence or classifier failure |
| `routing.default_engine` | `claude` | Configured engine name (`claude`, `opencode`, `codex`) |
| `[[target]].id` | `claude-haiku`, `claude-opus` | Unique target IDs; at least two targets required |
| `[[target]].engine` | `claude` | CLI adapter: `claude`, `opencode`, or `codex` |
| `[[target]].model` | `claude-haiku-4-5`, `claude-opus-4-6` | Model name passed to the engine CLI |
| `[[target]].efforts` | `low, medium` / `medium, high, xhigh, max` | Supported efforts (`none`, `low`, `medium`, `high`, `xhigh`, `max`); Claude excludes `none` |
| `[[target]].when` | task descriptions in starter config | Routing rubric for each target |

The default Jev backend sends at most `classifier.preview_bytes` of each prompt to TypeSafe. Use `heuristics` for no-network classification or a local `openai` endpoint for a local model; the engine CLI owns its own authentication. Full prompts and keys are not logged.

## Engine capability check

Run `mroute doctor` after `mroute init`, or `go run ./cmd/mroute doctor --config eval/heuristics.toml` for the committed offline catalogue. The probe asks each configured engine CLI for help; it does not launch a model request. Example on a machine with the supported Claude CLI:

```text
claude: supported (required flags: -p, --model, --effort)
```

Claude needs `-p`, `--model`, and `--effort`; opencode needs `run -m --variant`; Codex needs `exec -m -c`. A missing binary reports `command unavailable`; an older CLI whose help lacks a required flag reports e.g. `opencode: missing --variant (required flags: -m, --variant)` and `mroute doctor` exits nonzero. Update the engine CLI or choose supported targets before launching routed work.
