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

## Configuration and privacy

`mroute init` creates `$XDG_CONFIG_HOME/mroute/config.toml`, or `~/.config/mroute/config.toml` when XDG_CONFIG_HOME is unset. It refuses to overwrite the file; use `mroute init --force` to replace it. Each `[[target]]` declares an engine, model, supported efforts, and a short routing description. Claude targets cannot include `none` because the Claude CLI does not accept it. `routing.safe_target` identifies the fallback when classification is uncertain or fails.

The default `jev` classifier sends a bounded prompt preview (at most `classifier.preview_bytes`, initially 8000 bytes) to TypeSafe. Set `TYPESAFE_API_KEY` in the environment, not the config. Set `classifier.backend = "heuristics"` for offline routing without a key or network. `openai` can use a local OpenAI-compatible endpoint. Full prompts and keys are not written to route logs.

The CLI runs only one task at a time. Plugin recommendations in later milestones will not silently switch the model in an existing Claude Code or opencode session.

## Selection protocol (`mroute select`)

```sh
mroute select --json [--jev-model jev-1.13.0] < request.json > response.json
```

`mroute select` answers one selection request over caller-authorized candidates and writes exactly one JSON response line. It never reads `config.toml`, never writes the route log, never prints argv and never launches an engine; fallback and quota decisions stay with the caller. Empty and single-candidate requests need no model or key. Two or more candidates require a versioned `--jev-model` pin and `TYPESAFE_API_KEY`. Exit status is 0 for a selection or abstention, 1 for a selection error, and 2 for `invalid_request` (including two or more candidates without `--jev-model`) or bad arguments. The normative wire contract, limits and exit codes are in [`docs/contracts/selection-v1.md`](docs/contracts/selection-v1.md).

## M1 routing evaluation

Run `mroute eval eval/cases.jsonl` from a repository checkout with the desired backend configured. The 40 cases are hand-labeled examples of mechanical and complex coding tasks, not a representative production sample. On 2026-09-23, the starter target catalogue produced:

| Backend | Target accuracy | Effort within band | p50 | p95 |
| --- | ---: | ---: | ---: | ---: |
| Jev (`jev-latest`) | 92.5% | 95.0% | 288 ms | 360 ms |
| Heuristics | 62.5% | 32.5% | 0 ms | 0 ms |

Jev passed the M1 gate of 80% target accuracy, 75% effort-band accuracy, 1500 ms p50, and 2500 ms p95 on this seed set. Three Jev cases fell below the configured 0.60 choice-confidence gate and routed to the safe target. Results can shift when the `jev-latest` alias or network conditions change. The confidence gate and fallback are included in the reported accuracy; `mroute eval` uses the same decision path as `route` and `run`.
