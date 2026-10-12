#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."
mode="${1:-package}"
case "$mode" in
  build|package|universal) ;;
  *) echo "Usage: $0 [build|package|universal]" >&2; exit 2 ;;
esac

mkdir -p bin/native-build
architectures=(arm64)
if [[ "$mode" == universal ]]; then
  architectures=(arm64 x86_64)
fi

for architecture in "${architectures[@]}"; do
  go_arch="$architecture"
  if [[ "$architecture" == x86_64 ]]; then go_arch=amd64; fi
  target="bin/native-build/$architecture"
  mkdir -p "$target"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$go_arch" \
    CGO_CFLAGS="-mmacosx-version-min=13.0" \
    CGO_LDFLAGS="-mmacosx-version-min=13.0" \
    MACOSX_DEPLOYMENT_TARGET=13.0 \
    go build -tags native -trimpath -buildvcs=false \
      -ldflags="-w -s -extldflags=-mmacosx-version-min=13.0${APP_VERSION:+ -X main.appVersion=$APP_VERSION}" \
      -o "$target/iDeployBackend" .
  clang -arch "$architecture" -mmacosx-version-min=13.0 \
    -c updater_darwin.m -o "$target/updater.o"
  swiftc -swift-version 5 -target "$architecture-apple-macos13.0" \
    -import-objc-header macos/UpdaterBridge.h \
    macos/Sources/*.swift "$target/updater.o" -o "$target/iDeployUI"
done

if [[ "$mode" == build ]]; then
  echo "Built native SwiftUI and Go executables in bin/native-build/arm64"
  exit 0
fi

app="bin/iDeploy.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
if [[ "$mode" == universal ]]; then
  lipo -create -output "$app/Contents/MacOS/iDeployUI" \
    bin/native-build/arm64/iDeployUI bin/native-build/x86_64/iDeployUI
  lipo -create -output "$app/Contents/MacOS/iDeployBackend" \
    bin/native-build/arm64/iDeployBackend bin/native-build/x86_64/iDeployBackend
else
  cp bin/native-build/arm64/iDeployUI "$app/Contents/MacOS/iDeployUI"
  cp bin/native-build/arm64/iDeployBackend "$app/Contents/MacOS/iDeployBackend"
fi
cp build/darwin/icons.icns "$app/Contents/Resources/icons.icns"
if [[ -f build/darwin/Assets.car ]]; then
  cp build/darwin/Assets.car "$app/Contents/Resources/Assets.car"
fi
cp build/darwin/Info.plist "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundleExecutable iDeployUI' "$app/Contents/Info.plist"
codesign --force --deep --sign - "$app"
echo "Built $app"
