#!/usr/bin/env bash
# Builds release archives for all platforms into dist/.
# darwin binaries are signed with a Developer ID certificate and notarized when
#   SIGN_IDENTITY   (e.g. "Developer ID Application: Name (TEAMID)") and
#   NOTARY_PROFILE  (keychain profile created by `xcrun notarytool store-credentials`)
# are set. Without them the darwin binary is left unsigned.
set -euo pipefail

VERSION="$(cat VERSION)"
LDFLAGS="-s -w -X main.version=${VERSION}"
PLATFORMS=(darwin/arm64 linux/arm64 linux/amd64)
DIST=dist

rm -rf "$DIST"
mkdir -p "$DIST"

for p in "${PLATFORMS[@]}"; do
  os="${p%/*}"; arch="${p#*/}"
  name="helm-tui_${VERSION}_${os}_${arch}"
  dir="$DIST/$name"
  mkdir -p "$dir"

  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$dir/helm-tui" ./cmd/helm-tui

  if [[ "$os" == "darwin" && -n "${SIGN_IDENTITY:-}" ]]; then
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$dir/helm-tui"
    codesign --verify --strict --verbose=2 "$dir/helm-tui"
    if [[ -n "${NOTARY_PROFILE:-}" ]]; then
      zip="$DIST/notarize-$name.zip"
      ditto -c -k --keepParent "$dir/helm-tui" "$zip"
      xcrun notarytool submit "$zip" --keychain-profile "$NOTARY_PROFILE" --wait
      rm -f "$zip"
    fi
  fi

  cp README.md "$dir/"
  [[ -f LICENSE ]] && cp LICENSE "$dir/"
  tar -C "$DIST" -czf "$DIST/$name.tar.gz" "$name"
  rm -rf "$dir"
done

(cd "$DIST" && shasum -a 256 ./*.tar.gz | sed 's# \./# #' > checksums.txt && cat checksums.txt)
