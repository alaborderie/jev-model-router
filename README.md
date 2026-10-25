# mroute

`mroute` picks a coding CLI, model and effort level for a task, then launches that CLI for a **one-shot** run. You describe your targets once (for example "Claude Haiku at low effort for mechanical edits, Claude Opus at high effort for debugging and design"), and mroute classifies each prompt against that catalogue.

Why: most coding prompts don't need the most expensive model at maximum effort, and choosing by hand every time is tedious. mroute makes the choice explicit and inspectable — you can see the route, confidence and fallback reason before anything runs.

What it is not: it does not change the model of an active conversation, proxy provider APIs, or store credentials. Each engine CLI (`claude`, `opencode`, `codex`) keeps its own authentication.

## Install

Requires Go 1.27.1 or newer and at least one installed, authenticated engine CLI (`claude`, `opencode` or `codex`).

```sh
go install github.com/alaborderie/jev-model-router/cmd/mroute@latest
```

## Quick start

```sh
mroute init                       # writes ~/.config/mroute/config.toml
mroute doctor                     # checks each configured engine CLI
mroute route "Explain this error" # prints the recommendation, runs nothing
mroute run "Explain this error"   # launches the selected engine
mroute explain                    # shows the latest decision
```

`mroute log` shows the JSONL decision log. Use `mroute route --json "…"` for machine-readable output. To route prompt text that begins with a flag, put `--` before it: `mroute route -- --json` routes the literal prompt `--json`.

Without `TYPESAFE_API_KEY`, `init` selects the offline `heuristics` classifier, so you can try everything without a key or network access.

## How it works

1. The config lists `[[target]]` entries: engine, model, supported efforts, and a short `when` description.
2. A classifier scores the prompt against those descriptions and picks a target and an effort level:
   - `heuristics`: offline word overlap between the prompt and each target's `when` text, plus a few effort keywords.
   - `jev`: TypeSafe's hosted Jev model (`TYPESAFE_API_KEY`); sends a bounded prompt preview.
   - `openai`: any OpenAI-compatible endpoint, including a local model.
3. If confidence is below `classifier.confidence_gate`, or classification fails or times out, mroute falls back to `routing.safe_target`.
4. The decision (prompt hash, target, effort, confidence, reason, argv) is appended to a local log, and `run` executes the engine CLI with the chosen flags.

The same decision path backs the CLI, the Claude Code and opencode plugins, and `mroute eval`.

## Configuration and privacy

`mroute init` creates `$XDG_CONFIG_HOME/mroute/config.toml`, or `~/.config/mroute/config.toml` when XDG_CONFIG_HOME is unset. It selects offline `heuristics` when `TYPESAFE_API_KEY` is unset or empty and `jev` when present. Existing files are unchanged when the environment changes. It refuses to overwrite the file; use `mroute init --force` to replace it. Each `[[target]]` declares an engine, model, supported efforts, and a short routing description. Claude targets cannot include `none` because the Claude CLI does not accept it. `routing.safe_target` identifies the fallback when classification is uncertain or fails.

The optional `jev` classifier sends a bounded prompt preview (at most `classifier.preview_bytes`, initially 8000 bytes) to TypeSafe. Set `TYPESAFE_API_KEY` in the environment, not the config. Set `classifier.backend = "heuristics"` for offline routing without a key or network. `openai` can use a local OpenAI-compatible endpoint. Full prompts and keys are not written to route logs.

The CLI runs only one task at a time. Plugin recommendations never silently switch the model in an existing Claude Code or opencode session.

## Claude Code plugin

After installing `mroute` and running `mroute init`, add this repository as a Claude Code marketplace and install its plugin:

```sh
claude plugin marketplace add alaborderie/jev-model-router
claude plugin install mroute@mroute-marketplace
```

Restart Claude Code, then use `/mroute:route Explain this error` for a separate one-shot task. The plugin passes the prompt to the local `mroute` tool as structured data, which launches `mroute run`; it does not change the model of the current conversation. If the command is missing, confirm `mroute` is on Claude Code's `PATH` and that `mroute init` has created a config.

Installing and enabling the plugin also opts in to its `UserPromptSubmit` recommendation hook. The hook calls `mroute route` with a hard 1.8-second routing deadline (2 seconds at the Claude hook boundary), based on the measured 360 ms Jev p95. It adds a one-line recommendation to Claude's context; it does not switch models. If the CLI is missing, classification fails, or the deadline expires, it emits nothing and lets the prompt continue. To disable the hook, disable the plugin with `claude plugin disable mroute@mroute-marketplace`. The Jev backend sends the bounded prompt preview described above on each prompt; select `heuristics` in the config for an offline hook.

For Claude `Agent`/legacy `Task` subagent spawns, the plugin can set the subagent model to the routed Claude family (`haiku`, `sonnet`, or `opus`). It leaves an explicitly chosen model alone and skips unsupported event shapes, non-Claude targets, and classifier failures. The subagent gets a model for its own task; the active conversation never changes models mid-turn. Its `PreToolUse` hook shares the same 1.8-second routing deadline. Both hooks are enabled with the plugin and can be turned off by disabling it.

The config at `$XDG_CONFIG_HOME/mroute/config.toml` controls all plugin paths. `classifier.backend` chooses `jev`, `openai`, or `heuristics`; `classifier.confidence_gate`, `timeout_ms`, and `preview_bytes` control classification. `routing.safe_target` is the fallback target. Each `[[target]]` supplies `id`, `engine`, `model`, `efforts`, and `when`. The TypeSafe key belongs in `TYPESAFE_API_KEY` in the environment. If you use the Jev backend, the prompt hook sends a bounded preview of each submitted prompt to TypeSafe; `heuristics` sends nothing over the network.

## opencode plugin

Install `mroute` and initialize its config as above. Copy `plugin/opencode/mroute.js` to `~/.config/opencode/plugins/mroute.js` and `plugin/opencode/lib/route-core.mjs` to `~/.config/opencode/plugins/lib/route-core.mjs` (create the `plugins/lib` directory first), then restart opencode. Local plugins use opencode's `@opencode-ai/plugin` helper.

Ask the agent to use the `route` tool with a prompt to launch a **separate one-shot task** via `mroute run`; its output is returned to the agent. Delegated work has no plugin-imposed task deadline; opencode cancellation terminates the child. The tool drains output while retaining at most 10 MiB of stdout for the result (with a truncation notice), so a large response does not terminate work. Engine stderr is drained without being returned. The `recommend` tool runs `mroute route --json` with a 2-second probe deadline, shows a toast and appends the recommendation to the TUI prompt when those surfaces are available. The recommendation also appears in the tool result if the TUI is unavailable. Neither tool switches the model of the active conversation. The configured Jev backend sends the bounded preview described under Configuration and privacy; use `heuristics` for offline routing. Run `node --test plugin/opencode/route.test.mjs` to exercise the local tool fixture.

## Selection protocol (`mroute select`)

```sh
mroute select --json [--jev-model jev-1.13.0] < request.json > response.json
```

`mroute select` answers one selection request over caller-authorized candidates and writes exactly one JSON response line. It never reads `config.toml`, never writes the route log, never prints argv and never launches an engine; fallback and quota decisions stay with the caller. Empty and single-candidate requests need no model or key. Two or more candidates require a versioned `--jev-model` pin and `TYPESAFE_API_KEY`. Exit status is 0 for a selection or abstention, 1 for a selection error, and 2 for `invalid_request` (including two or more candidates without `--jev-model`) or bad arguments. The normative wire contract, limits and exit codes are in [`docs/contracts/selection-v1.md`](docs/contracts/selection-v1.md).

## Routing evaluation

Run `mroute eval eval/cases.jsonl` from a repository checkout with the desired backend configured. The 40 cases are hand-labeled examples of mechanical and complex coding tasks, not a representative production sample. On 2026-09-23, the starter target catalogue produced:

For a deterministic offline table, run `go run ./cmd/mroute eval --table --config eval/heuristics.toml eval/cases.jsonl`; the generated output is committed at [eval/results.md](eval/results.md). The Jev figures below are a historical live run, not a deterministic replay: the `jev-latest` alias and latency may change.

| Backend | Target accuracy | Effort within band | p50 | p95 |
| --- | ---: | ---: | ---: | ---: |
| Jev (`jev-latest`) | 92.5% | 95.0% | 288 ms | 360 ms |
| Heuristics | 62.5% | 32.5% | 0 ms | 0 ms |

Jev passed the project's acceptance gate of 80% target accuracy, 75% effort-band accuracy, 1500 ms p50, and 2500 ms p95 on this seed set. Three Jev cases fell below the configured 0.60 choice-confidence gate and routed to the safe target. Results can shift when the `jev-latest` alias or network conditions change. The confidence gate and fallback are included in the reported accuracy; `mroute eval` uses the same decision path as `route` and `run`.

## Configuration reference

`mroute init` writes the starter values below; a custom TOML file must supply the required fields (there are no implicit config defaults). `mroute eval --config FILE` and `mroute doctor --config FILE` let you inspect a particular catalogue without changing your personal config.

| Field | Starter value | Meaning |
| --- | --- | --- |
| `classifier.backend` | `heuristics` without a key; `jev` with a key | `jev`, `openai`, or offline `heuristics` |
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

The Jev backend sends at most `classifier.preview_bytes` of each prompt to TypeSafe. Use `heuristics` for no-network classification or a local `openai` endpoint for a local model; the engine CLI owns its own authentication. Full prompts and keys are not logged.

## Engine capability check

Run `mroute doctor` after `mroute init`, or `go run ./cmd/mroute doctor --config eval/heuristics.toml` for the committed offline catalogue. The probe asks each configured engine CLI for help; it does not launch a model request. Example on a machine with the supported Claude CLI:

```text
claude: supported (required flags: -p, --model, --effort)
```

Claude needs `-p`, `--model`, and `--effort`; opencode needs `run -m --variant`; Codex needs `exec -m -c`. A missing binary reports `command unavailable`; an older CLI whose help lacks a required flag reports e.g. `opencode: missing --variant (required flags: -m, --variant)` and `mroute doctor` exits nonzero. Update the engine CLI or choose supported targets before launching routed work.

## Website

`mroute serve` runs the static landing page at <https://jev-model-router.antoinelaborderie.com/> (port 8080, with `GET /healthz`). It does not accept prompts or expose a routing API. The `Dockerfile` builds that server; images are published to `ghcr.io/alaborderie/jev-model-router` on pushes to `main`. [Offline route receipt](distribution/offline-receipt.md) documents a captured local run.

## Development

```sh
go build ./...
go vet ./...
go test ./...
node --test plugin/opencode/route.test.mjs
scripts/package-release.sh dist   # linux/darwin × amd64/arm64 archives + SHA256SUMS
```

## License

[MIT](LICENSE)
