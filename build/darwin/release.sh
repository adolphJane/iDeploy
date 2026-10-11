#!/bin/bash
set -euo pipefail

# Run from the repository root. Sparkle signs the archive with the dedicated
# Ed25519 key; Apple Developer ID signing can be added separately later.
tag="${1:?Usage: bash build/darwin/release.sh v1.2.3}"
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  echo "Expected a version tag such as v1.2.3 or v1.2.3-beta.1" >&2
  exit 1
fi
version="${tag#v}"
bundle_version="${version%%-*}"
build_number="${APP_BUILD_NUMBER:-${GITHUB_RUN_NUMBER:-}}"
if [[ ! "$build_number" =~ ^[1-9][0-9]*$ ]]; then
  echo "APP_BUILD_NUMBER (or GITHUB_RUN_NUMBER) must be a positive, increasing integer" >&2
  exit 1
fi
if [[ "${GITHUB_ACTIONS:-}" == true && -z "${SPARKLE_ED25519_PRIVATE_KEY:-}" ]]; then
  echo "Missing SPARKLE_ED25519_PRIVATE_KEY Actions Secret" >&2
  exit 1
fi
sparkle_cache="$(bash build/darwin/fetch-sparkle.sh)"
wails3 task darwin:package:universal "APP_VERSION=$version"
app="bin/iDeploy.app"
framework="$app/Contents/Frameworks/Sparkle.framework"
mkdir -p "$app/Contents/Frameworks"
ditto "$sparkle_cache/Sparkle.framework" "$framework"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $bundle_version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $build_number" "$app/Contents/Info.plist"
public_key="$(tr -d '\n' < build/darwin/sparkle-public-key.txt)"
/usr/libexec/PlistBuddy -c "Add :SUFeedURL string https://github.com/adolphJane/iDeploy/releases/latest/download/appcast.xml" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add :SUPublicEDKey string $public_key" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add :SUEnableAutomaticChecks bool true" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add :SUAutomaticallyUpdate bool true" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add :SUVerifyUpdateBeforeExtraction bool true" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Add :SURequireSignedFeed bool true" "$app/Contents/Info.plist"

# Sign Sparkle's nested helpers before the framework and the outer app.
codesign --force --sign - "$framework/Versions/B/XPCServices/Installer.xpc"
codesign --force --sign - --preserve-metadata=entitlements "$framework/Versions/B/XPCServices/Downloader.xpc"
codesign --force --sign - "$framework/Versions/B/Autoupdate"
codesign --force --sign - "$framework/Versions/B/Updater.app"
codesign --force --sign - "$framework"
codesign --force --sign - "$app"
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
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
cp "bin/release/$archive" "$stage/"
appcast_args=(--download-url-prefix "https://github.com/adolphJane/iDeploy/releases/download/$tag/")
if [[ -n "${SPARKLE_ED25519_PRIVATE_KEY:-}" ]]; then
  printf '%s' "$SPARKLE_ED25519_PRIVATE_KEY" | go run build/darwin/verify_sparkle_key.go "$public_key"
  printf '%s' "$SPARKLE_ED25519_PRIVATE_KEY" | "$sparkle_cache/bin/generate_appcast" \
    --ed-key-file - "${appcast_args[@]}" "$stage"
else
  umask 077
  "$sparkle_cache/bin/generate_keys" --account com.ideploy.app -x "$stage/private-key.txt"
  go run build/darwin/verify_sparkle_key.go "$public_key" < "$stage/private-key.txt"
  "$sparkle_cache/bin/generate_appcast" --ed-key-file "$stage/private-key.txt" \
    "${appcast_args[@]}" "$stage"
fi
test -s "$stage/appcast.xml"
cp "$stage/appcast.xml" bin/release/appcast.xml
