#!/bin/bash
set -euo pipefail

# Run from the repository root. No Apple account or certificate is required.
tag="${1:?Usage: bash build/darwin/release.sh v1.2.3}"
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  echo "Expected a version tag such as v1.2.3 or v1.2.3-beta.1" >&2
  exit 1
fi
version="${tag#v}"
bundle_version="${version%%-*}"
wails3 task darwin:package:universal "APP_VERSION=$version"
app="bin/iDeploy.app"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $bundle_version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $bundle_version" "$app/Contents/Info.plist"
# Editing Info.plist invalidates the original ad-hoc signature.
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict --verbose=2 "$app"
architectures="$(lipo -archs "$app/Contents/MacOS/iDeploy")"
for architecture in arm64 x86_64; do
  if [[ " $architectures " != *" $architecture "* ]]; then
    echo "Missing required architecture: $architecture ($architectures)" >&2
    exit 1
  fi
done
mkdir -p bin/release
archive="iDeploy-${tag}-macos-universal.zip"
ditto -c -k --sequesterRsrc --keepParent "$app" "bin/release/$archive"
(cd bin/release && shasum -a 256 "$archive" > SHA256SUMS.txt)
