#!/bin/bash
set -euo pipefail

# Pin the framework and release tools to one reviewed Sparkle distribution.
version=2.10.0
sha256=c2bf58aa8387266ac179357b1415d6f2635f044da8be41042af32425dae6da0c
cache="${SPARKLE_CACHE_DIR:-bin/sparkle-$version}"
archive="$cache/Sparkle-$version.tar.xz"
mkdir -p "$cache"
extract=false
if [[ ! -f "$archive" ]] || ! printf '%s  %s\n' "$sha256" "$archive" | shasum -a 256 --check >/dev/null 2>&1; then
  curl --http1.1 --fail --location --retry 3 --max-time 180 --silent --show-error \
    "https://github.com/sparkle-project/Sparkle/releases/download/$version/Sparkle-$version.tar.xz" \
    --output "$archive"
  printf '%s  %s\n' "$sha256" "$archive" | shasum -a 256 --check >/dev/null
  extract=true
fi
if [[ "$extract" == true || ! -d "$cache/Sparkle.framework" || ! -x "$cache/bin/generate_appcast" || ! -x "$cache/bin/generate_keys" ]]; then
  tar -xJf "$archive" -C "$cache" ./Sparkle.framework ./bin/generate_appcast ./bin/generate_keys ./bin/sign_update
fi
printf '%s\n' "$cache"
