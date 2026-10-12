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
APP_VERSION="$version" bash build/darwin/build-native.sh universal
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
for executable in iDeployUI iDeployBackend; do
  architectures="$(lipo -archs "$app/Contents/MacOS/$executable")"
  for architecture in arm64 x86_64; do
    if [[ " $architectures " != *" $architecture "* ]]; then
      echo "Missing required architecture: $architecture ($executable: $architectures)" >&2
      exit 1
    fi
  done
done
mkdir -p bin/release
archive="iDeploy-${tag}-macos-universal.zip"
ditto -c -k --sequesterRsrc --keepParent "$app" "bin/release/$archive"
disk_image="iDeploy-${tag}-macos-universal.dmg"
stage="$(mktemp -d)"
appcast_stage="$(mktemp -d)"
trap 'rm -rf "$stage" "$appcast_stage"' EXIT
ditto "$app" "$stage/iDeploy.app"
ln -s /Applications "$stage/Applications"
hdiutil create -volname "iDeploy $tag" -srcfolder "$stage" -ov -format UDZO "bin/release/$disk_image"
hdiutil verify "bin/release/$disk_image" >/dev/null

cp "bin/release/$archive" "$appcast_stage/"
appcast_args=(--download-url-prefix "https://github.com/adolphJane/iDeploy/releases/download/$tag/")
if [[ -n "${SPARKLE_ED25519_PRIVATE_KEY:-}" ]]; then
  printf '%s' "$SPARKLE_ED25519_PRIVATE_KEY" | go run build/darwin/verify_sparkle_key.go "$public_key"
  printf '%s' "$SPARKLE_ED25519_PRIVATE_KEY" | "$sparkle_cache/bin/generate_appcast" \
    --ed-key-file - "${appcast_args[@]}" "$appcast_stage"
else
  umask 077
  "$sparkle_cache/bin/generate_keys" --account com.ideploy.app -x "$appcast_stage/private-key.txt"
  go run build/darwin/verify_sparkle_key.go "$public_key" < "$appcast_stage/private-key.txt"
  "$sparkle_cache/bin/generate_appcast" --ed-key-file "$appcast_stage/private-key.txt" \
    "${appcast_args[@]}" "$appcast_stage"
fi
test -s "$appcast_stage/appcast.xml"
cp "$appcast_stage/appcast.xml" bin/release/appcast.xml
(cd bin/release && shasum -a 256 "$archive" "$disk_image" > SHA256SUMS.txt)
