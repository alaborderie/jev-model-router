#!/usr/bin/env python3
"""Reference candidate_set_hash for mroute selection protocol v1 (stdlib only).

Usage: python3 -I testdata/selection-v1/reference_hash.py FILE...
Prints "<hex>  <file>" per request file. Exits 1 on the first unusable file
without echoing its content.
"""
import hashlib
import json
import sys


def _object(pairs):
    out = {}
    for key, value in pairs:
        if key in out:
            raise ValueError("duplicate key")
        out[key] = value
    return out


def _reject(_):
    raise ValueError("non-integer number")


def candidate_set_hash(raw):
    if raw.startswith(b"\xef\xbb\xbf"):
        raise ValueError("byte-order mark")
    doc = json.loads(raw.decode("utf-8"), object_pairs_hook=_object, parse_float=_reject, parse_constant=_reject)
    candidates = doc["candidates"]
    ids = [c["candidate_id"] for c in candidates]
    if len(set(ids)) != len(ids):
        raise ValueError("duplicate candidate_id")
    ordered = sorted(candidates, key=lambda c: c["candidate_id"])
    encoded = json.dumps(ordered, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def main(paths):
    if not paths:
        print("usage: reference_hash.py FILE...", file=sys.stderr)
        return 2
    for path in paths:
        try:
            with open(path, "rb") as f:
                digest = candidate_set_hash(f.read())
        except (OSError, ValueError, KeyError, TypeError, UnicodeError):
            print(f"reference_hash: invalid request: {path}", file=sys.stderr)
            return 1
        print(f"{digest}  {path}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
