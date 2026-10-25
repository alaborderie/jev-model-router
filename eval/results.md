# Reproducible offline baseline

Generated from the committed 40-case `eval/cases.jsonl` with the committed starter-equivalent offline catalogue:

```sh
go run ./cmd/mroute eval --table --config eval/heuristics.toml eval/cases.jsonl
```

| Backend | Model | Cases | Target accuracy | Effort within band |
| --- | --- | ---: | ---: | ---: |
| heuristics | built-in | 40 | 62.5% | 32.5% |

The `--table` output omits wall-clock timing so identical inputs produce the same table. The heuristic baseline misses the Jev acceptance gates (80% target, 75% effort). The historical Jev run in the README passed those gates on 2026-09-23; it is not included in this offline reproduction, since `jev-latest` and network latency can change. The cases are a small hand-labeled seed set, not a production benchmark.
