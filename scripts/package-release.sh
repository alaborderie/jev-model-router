#!/usr/bin/env bash
set -euo pipefail
# Offline packaging only: no tag, release, signing key or upload.
out=${1:?usage: scripts/package-release.sh OUTPUT_DIRECTORY}
mkdir -p "$out"
out=$(cd "$out" && pwd)
for release_os in linux darwin; do
  for release_arch in amd64 arm64; do
    stage="$out/mroute_${release_os}_${release_arch}"
    mkdir -p "$stage"
    CGO_ENABLED=0 GOOS="$release_os" GOARCH="$release_arch" go build -trimpath -o "$stage/mroute" ./cmd/mroute
    cp LICENSE README.md "$stage/"
    tar -czf "$stage.tar.gz" -C "$stage" mroute LICENSE README.md
  done
done
(cd "$out" && sha256sum ./*.tar.gz > SHA256SUMS)
