# Distribution drafts — review before external publication

Source repository: https://github.com/alaborderie/f-0923-jev-model-router

## opencode ecosystem contribution

Target: https://github.com/anomalyco/opencode/blob/dev/packages/web/src/content/docs/ecosystem.mdx (PR against `anomalyco/opencode`, `dev` branch). Apply [opencode-ecosystem.patch](opencode-ecosystem.patch) to an upstream checkout and refresh its context before submission. This is a draft, not an upstream PR.

**Draft PR title:** docs: list mroute one-shot model router

**Draft PR body:**

> Adds mroute to the plugin ecosystem list. It is a local CLI and opencode plugin with `route` and `recommend` tools. Install the binary with `go install github.com/alaborderie/f-0923-jev-model-router/cmd/mroute@latest`, run `mroute init`, then copy the plugin files as described in the [README](https://github.com/alaborderie/f-0923-jev-model-router#opencode-plugin). `route` launches a separate one-shot engine task; `recommend` displays a TUI recommendation. It does not change the active session's model. [Offline 40-case evaluation and reproduction command](https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md) are published alongside the code. The default Jev classifier sends a bounded prompt preview; users can select offline heuristics.

## Claude Code marketplace and ClaudePluginHub

- Repository marketplace manifest: https://github.com/alaborderie/f-0923-jev-model-router/blob/main/.claude-plugin/marketplace.json
- Plugin manifest: https://github.com/alaborderie/f-0923-jev-model-router/blob/main/plugin/claude/.claude-plugin/plugin.json
- Installation: `claude plugin marketplace add alaborderie/f-0923-jev-model-router`, then `claude plugin install mroute@mroute-marketplace`, after installing the CLI and running `mroute init` (see [README](https://github.com/alaborderie/f-0923-jev-model-router#claude-code-plugin)).
- ClaudePluginHub submission target: https://claudepluginhub.com/tools/submit-plugin — submit the GitHub repository URL after the public repository and manifest are available; sign-in or indexing is handled by the publisher, not this draft.
- Suggested listing title: **mroute — local one-shot model-and-effort router**.
- Suggested description: **Run coding tasks via a local CLI-selected engine/model/effort in Claude Code and opencode. Claude Code also recommends the next route and can select a subagent model; it does not switch the active conversation's model. Configurable offline heuristic mode; default Jev mode sends a bounded preview to TypeSafe.**
- Evaluation reference: https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md. The offline baseline is 62.5% target and 32.5% effort-band accuracy on 40 hand-labeled cases; historical Jev results are identified separately in the README.

## Tracking-issue reply drafts

These are proposals for review, not posted comments. Check the thread status immediately before posting; the Anthropic issue is currently closed.

### https://github.com/anthropics/claude-code/issues/67898

> A local experiment for the related routing use case: [mroute](https://github.com/alaborderie/f-0923-jev-model-router) launches a separate one-shot task with a classified model/effort and can route Claude subagent spawns; its prompt hook only recommends. It does **not** change the active conversation's model per turn, so native support requested here would still be useful. The [40-case offline baseline](https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md) and historical Jev run are documented separately. The issue is closed; retain this draft unless maintainers welcome follow-up.

### https://github.com/anomalyco/opencode/issues/45764

> [mroute](https://github.com/alaborderie/f-0923-jev-model-router) offers `route` and `recommend` plugin tools using supported APIs: the first launches a one-shot CLI task, and the second shows a toast/appends a recommendation. It cannot intercept or override the current LLM request's model; the hook requested here would enable a different integration. [Reproducible offline eval](https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md) and the one-shot scope are in the README.

### https://github.com/anomalyco/opencode/issues/34370

> A bounded piece of this idea exists in [mroute](https://github.com/alaborderie/f-0923-jev-model-router): explicit `route` delegates a one-shot task to an engine/model/effort, while `recommend` shows a TUI suggestion. It does not provide automatic turn-level intent routing, vision delegation, or an advisor. The [offline eval set and results](https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md) are public for comparison.

### https://github.com/anomalyco/opencode/issues/50729

> [mroute](https://github.com/alaborderie/f-0923-jev-model-router) is a local one-shot routing plugin rather than a `chat.model` implementation. Its recommendation tool can append text and show a toast, but it cannot change the active session's model. A supported per-step model hook would still be necessary for that use case. [README and reproducible offline eval](https://github.com/alaborderie/f-0923-jev-model-router/blob/main/eval/results.md).
