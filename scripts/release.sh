#!/usr/bin/env bash
# Builds release archives for every platform into ./release.
#   scripts/release.sh v0.1.0
# Needs Go and Node. The page is built once and embedded in each binary.
set -euo pipefail

version="${1:?usage: scripts/release.sh vX.Y.Z}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/release"
cd "$root"

rm -rf "$out"
mkdir -p "$out"

(cd web && npm ci && npm run build)

platforms="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"
for p in $platforms; do
  os="${p%/*}"
  arch="${p#*/}"
  name="issue-mapper_${version}_${os}_${arch}"
  stage="$out/$name"
  mkdir -p "$stage"
  bin="issue-mapper"
  [ "$os" = "windows" ] && bin="issue-mapper.exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w -X main.version=${version}" -o "$stage/$bin" ./cmd/issue-mapper
  cp LICENSE README.md "$stage/"
  if [ "$os" = "windows" ]; then
    (cd "$out" && zip -qr "$name.zip" "$name")
  else
    tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  fi
  rm -rf "$stage"
done

(cd "$out" && sha256sum ./*.tar.gz ./*.zip > checksums.txt)
echo "built:"
ls -1 "$out"
