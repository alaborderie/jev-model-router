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

`route` and `run` are delivered later in M1. Until then, `init` is the available command.

## Configuration and privacy

`mroute init` creates `$XDG_CONFIG_HOME/mroute/config.toml`, or `~/.config/mroute/config.toml` when XDG_CONFIG_HOME is unset. It refuses to overwrite the file; use `mroute init --force` to replace it. Each `[[target]]` declares an engine, model, supported efforts, and a short routing description. `routing.safe_target` identifies the fallback when classification is uncertain or fails.

The default `jev` classifier sends a bounded prompt preview (at most `classifier.preview_bytes`, initially 8000 bytes) to TypeSafe. Set `TYPESAFE_API_KEY` in the environment, not the config. Set `classifier.backend = "heuristics"` for offline routing without a key or network. `openai` can use a local OpenAI-compatible endpoint. Full prompts and keys are not written to route logs.

The CLI runs only one task at a time. Plugin recommendations in later milestones will not silently switch the model in an existing Claude Code or opencode session.
