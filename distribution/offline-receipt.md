# Captured offline route — 2026-10-16

Captured from the launch-readiness branch based on `4e151bd`, using the unchanged
`eval/heuristics.toml` catalogue and built-in heuristics. This is an actual local
recommendation, not engine execution or a live Jev result. No key or paid API call.
The bounded preview is classifier input; the logged receipt stores a hash, not
prompt text. Argv excludes the prompt in the log.

Synthetic prompt: `Rename foo to bar`.

```sh
mkdir -p .local-demo/mroute
cp eval/heuristics.toml .local-demo/mroute/config.toml
XDG_CONFIG_HOME="$PWD/.local-demo" XDG_STATE_HOME="$PWD/.local-demo" go run ./cmd/mroute route --json -- 'Rename foo to bar'
XDG_CONFIG_HOME="$PWD/.local-demo" XDG_STATE_HOME="$PWD/.local-demo" go run ./cmd/mroute explain
```

Actual route output (timestamp is this capture, not a fixture expectation):

```json
{"timestamp":"2026-10-16T05:50:24.493830808Z","query_hash":"dfabc4bf5b2758d94fc13fd6607f707aa9d6ffc1fe79abcb9f77c61305bb344c","target_id":"claude-opus","engine":"claude","model":"claude-opus-4-6","effort":"medium","choice_confidence":1,"score_confidence":1,"classifier_backend":"heuristics","classifier_latency_ms":0,"outcome":"ok","reason":"classified","argv":["claude","-p","--model","claude-opus-4-6","--effort","medium","--"]}
```

The catalogue selected `claude-opus` with medium effort. This result is not a quality endorsement or a claim
that renaming requires that model. The 40-case eval rerun returned 62.5% target
accuracy and 32.5% effort within band. Do not turn a single receipt into an
accuracy claim. Review and edit the catalogue before a deliberate `run`.

## Before/after page inventory

Preserved stylesheet, skip link, all original section IDs, route/run/explain/log
examples, private GitHub/docs/issues notices, configuration and both plugin links.
Changed title, H1 and classifier setup description; added canonical/OG/schema,
receipt/privacy sections and robots/sitemap routes. No auth, hosted API, analytics,
form, image, iframe or third-party script added. No private-preview notice removed.
The founder must approve this page and change list before merge/deployment.
