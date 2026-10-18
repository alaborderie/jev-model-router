# mroute selection protocol, version 1

Status: normative. Shared by mroute (`internal/selection`, `cmd/mroute/select.go`) and
the agent-startup consumer. Fixtures live in `testdata/selection-v1/`.

`mroute select` asks Jev one Choice question over caller-authorized candidates. It
never loads the user target catalogue (`config.toml`), never writes the route log,
never prints argv and never launches an engine. Fallback, admission and quota
reservation belong to the caller.

## Command

```sh
mroute select --json [--jev-model <versioned-id>] < request.json > response.json
```

- `--json` is required; it is the only output format.
- `--jev-model` is the trusted model pin from consumer policy. It must be a versioned
  Jev ID matching `^jev-[0-9]+\.[0-9]+\.[0-9]+$` (for example `jev-1.13.0`;
  `selection.ValidJevModel`); the moving aliases `jev-latest` and `jev-preview` are
  rejected because the confidence gate is only meaningful against a pinned model
  version. It is required only when the request has two
  or more candidates; empty and singleton requests complete without a model or key.
- The endpoint is the TypeSafe production endpoint; neither the request nor the
  command line can override it. The API key is read from `TYPESAFE_API_KEY` by name
  only when inference is needed.
- Deadlines: the whole command 3 seconds, the single Jev HTTP request 2 seconds. One
  HTTP request at most; no retry. SIGINT/SIGTERM cancel the request.

## Request

stdin holds exactly one UTF-8 JSON object (no byte-order mark), optionally surrounded
by JSON whitespace and nothing else, at most 65536 bytes in total. All fields are required; unknown keys at the
top level and in candidates are rejected. Duplicate object keys are rejected at every
depth, including inside `role_brief` and `state`.

| Field | Type | Rule |
| --- | --- | --- |
| `schema_version` | integer | exactly `1` |
| `request_id` | Id | |
| `candidate_set_hash` | string | 64 lowercase hex; equals the canonical hash below |
| `role_policy_version` | Id | |
| `role_brief` | object | Structured value; canonical size ≤ 4096 bytes |
| `state` | object | Structured value; canonical size ≤ 12288 bytes |
| `candidates` | array | 0–32 Candidates, unique `candidate_id` |

Candidate fields (exactly these ten):

| Field | Type | Rule |
| --- | --- | --- |
| `candidate_id` | Id | unique in the request |
| `access_id` | Id | |
| `engine` | Id | |
| `provider_id` | Id | |
| `model_id` | Id | |
| `transport` | Id | |
| `variant` | Id or `null` | `null` is a provider-default candidate; the key is still required |
| `capability_profile_version` | Id | |
| `configuration_revision` | integer | 1 ≤ n ≤ 9007199254740991 |
| `description` | string | 1–1024 UTF-8 bytes; quota/suitability facts only |

Definitions:

- **Id**: nonempty ASCII string of at most 160 bytes over `[A-Za-z0-9._:/-]`.
  Case-sensitive and opaque. Never a credential, credential hash, email or
  authenticated URL.
- **Integer**: a JSON number token written with digits only (no sign, fraction or
  exponent, no leading zero).
- **Structured value**: JSON object, array, string, boolean or `null`, nested at most
  32 levels deep (the `role_brief`/`state` object itself is level 1; each nested object
  or array adds a level, strings, booleans and `null` do not). Numbers are not
  allowed anywhere inside `role_brief` or `state`; send numeric observations as decimal
  strings. The size bound applies to the canonical encoding of that value.
- **Strings** must be valid Unicode: invalid UTF-8 bytes and unpaired surrogate
  escapes (`\ud800` without a following low surrogate, or a lone low surrogate) are
  rejected. Any other character, including controls written as escapes, is allowed in
  `description`, `role_brief` and `state`.

`role_brief` comes from trusted, versioned consumer policy. `state` and descriptions
can carry venture text; mroute passes them to Jev as data, never as instructions.

## Canonical encoding and candidate hash

`candidate_set_hash` = lowercase hex SHA-256 of the canonical encoding of the
`candidates` array after sorting it by `candidate_id` (byte order of the ASCII IDs).

Canonical encoding (equal to Python
`json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")`):

- No insignificant whitespace.
- Object keys sorted by Unicode code point (equivalently, UTF-8 byte order), recursively.
- Integers in plain decimal. No other numbers occur in hashed or measured values.
- `true`, `false`, `null` literally.
- Strings: `"` → `\"`, `\` → `\\`, U+0008 → `\b`, U+000C → `\f`, U+000A → `\n`,
  U+000D → `\r`, U+0009 → `\t`, every other code point below U+0020 → `\u00XX` with
  **lowercase** hex. Every other character, including `<`, `>`, `&`, U+007F, U+2028,
  U+2029 and non-BMP characters, is written literally as UTF-8. A literal backslash
  followed by `u` in the source text is the two characters `\\u`.
- A null `variant` is encoded as `"variant":null`; the key is never omitted.

The reference implementation is `testdata/selection-v1/reference_hash.py`; the Go
implementation is `selection.CandidateHash`. Both must produce every hash committed
in `testdata/selection-v1/manifest.json`.

## Response

On any validated request stdout carries exactly one JSON object, followed by one
newline, with these keys in this order:

```json
{"schema_version":1,"request_id":"…","candidate_set_hash":"…","status":"selected","candidate_id":"…","reason":"classified","confidence":0.83,"selector_model":"jev-1.13.0","latency_ms":312,"usage":{"input_tokens":512,"output_tokens":1}}
```

| Field | Rule |
| --- | --- |
| `schema_version` | `1` |
| `request_id` | Id; copied from the validated request |
| `candidate_set_hash` | 64 lowercase hex; copied from the validated request |
| `status` | `selected`, `abstained` or `error` |
| `candidate_id` | an in-request ID when `selected`; otherwise `null` |
| `reason` | closed code, see below |
| `confidence` | `null` or finite number in [0, 1] |
| `selector_model` | `null` or the versioned Jev ID that answered, matching `^jev-[0-9]+\.[0-9]+\.[0-9]+$` |
| `latency_ms` | nonnegative integer, time spent selecting |
| `usage` | `null` or `{"input_tokens":n,"output_tokens":n}`, n nonnegative integers |

Reasons, by status:

| Status | Reason | Meaning |
| --- | --- | --- |
| `selected` | `singleton` | exactly one candidate; no inference; confidence, model and usage `null` |
| `selected` | `classified` | Jev chose with confidence ≥ 0.60; confidence and model non-null |
| `abstained` | `no_candidates` | empty candidate list; no inference; confidence, model and usage `null` |
| `abstained` | `low_confidence` | Jev answered with confidence < 0.60; confidence and model non-null |
| `error` | `invalid_request` | request valid but not servable (multi-candidate without `--jev-model`) |
| `error` | `invalid_response` | Jev returned malformed, untyped, out-of-set or wrong-model output |
| `error` | `authentication` | no API key, or HTTP 401/403 |
| `error` | `rate_limit` | HTTP 429 |
| `error` | `overloaded` | HTTP 529 or 503 |
| `error` | `network` | connection failure or any other HTTP status |
| `error` | `timeout` | the 2 s HTTP or 3 s command deadline elapsed |
| `error` | `cancelled` | the command received SIGINT/SIGTERM |

**Confidence gate.** The gate is `0.60`, a protocol-v1 constant applied against the
pinned model version (`--jev-model`). It is not a request field, flag or consumer
setting; changing it requires a new protocol version. `classified` requires
confidence ≥ 0.60 and `low_confidence` requires confidence < 0.60; a response that
contradicts the gate is invalid.

`classified` and `low_confidence` occur only with two or more candidates, `singleton`
only with one, `no_candidates` only with zero; `error`/`invalid_request` only with two
or more. On `error`, `candidate_id` and `confidence` are `null`; `selector_model` and
`usage` are `null` unless Jev returned them in a typed answer that failed validation,
so they may be non-null only with reason `invalid_response` and only when the request
has two or more candidates (no inference happens otherwise). The caller must check
that `request_id` is an Id and `candidate_set_hash` is 64 lowercase hex, that both
equal its own request's values, and refuse a response for another request even if its
`candidate_id` is valid (`selection.ValidateResponse`, which also enforces the gate
and the `selector_model` grammar).

A selection error never falls back to another candidate inside mroute; mroute's
legacy `routing.safe_target` is never consulted on this path.

## Exit status and stderr

| Exit | When | stdout | stderr |
| --- | --- | --- | --- |
| 0 | `selected` or `abstained` | response | empty |
| 1 | `error` other than `invalid_request` | response | one line `mroute: select: <reason>` |
| 2 | `error` with `invalid_request` | response | one line `mroute: select: invalid_request` |
| 2 | malformed, oversized or invalid input; bad arguments | empty | one line `mroute: select: invalid_request` or `mroute: select: usage` |

stderr never contains request content, provider response bodies, URLs or credentials.

## Jev question

One `choice` question keyed `candidate`. Criteria map each `candidate_id` to its
`description`. Instructions are a fixed selection question plus the role policy
version and `role_brief`. TypeSafe `state` is
`{"role_policy_version": …, "venture_state": <state>}`. No effort score is asked.
The returned `model` must equal the pin; `choice` must be an in-request ID;
`confidence` must be finite in [0, 1].

## Fixtures

`testdata/selection-v1/` holds request fixtures (`*.json`, `invalid/*.json`),
response fixtures (`responses/*.json`, `responses/invalid/*.json`), the reference
hasher `reference_hash.py`, and `manifest.json`. Every fixture file appears in the
manifest exactly once. The manifest is a JSON array of entries with exactly these
keys:

| Key | Type | Meaning |
| --- | --- | --- |
| `file` | string | path relative to `testdata/selection-v1/`, `/`-separated |
| `kind` | `"request"` or `"response"` | |
| `valid` | boolean | whether the fixture must be accepted |
| `request` | string or `null` | for responses, the request fixture it answers; `null` for requests |
| `candidate_set_hash` | string or `null` | the expected hash for valid requests; otherwise `null` |
| `error` | string or `null` | `null` when `valid`; otherwise the expected error code |

`error` is the bare code after the sentinel prefix: mroute's `Decode` returns
`invalid_request: <code>` for requests and `ValidateResponse` returns
`invalid_response: <code>` for responses, so `kind` determines the prefix and the
manifest omits it (for example `"error": "duplicate_key"`). These codes are
informative only, for mroute's own tests; they are not part of the CLI interface. The
CLI prints only `mroute: select: invalid_request` for any rejected request, and other
implementations need only agree on `valid`, not on the code.

Regenerate with `MROUTE_WRITE_SELECTION_FIXTURES=1 go test ./internal/selection -run
TestWriteFixtures`; the valid request hashes must not change without a protocol
version bump.
