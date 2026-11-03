#!/usr/bin/env bash
# Prints the Homebrew formula for the release in dist/ (version from VERSION,
# sha256 from dist/checksums.txt). Usage: scripts/formula.sh > Formula/helm-tui.rb
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="$(cat VERSION)"
sum() { awk -v f="helm-tui_${VERSION}_$1.tar.gz" '$2==f {print $1}' dist/checksums.txt; }
for t in darwin_arm64 linux_arm64 linux_amd64; do
  [ -n "$(sum $t)" ] || { echo "no checksum for $t in dist/checksums.txt" >&2; exit 1; }
done
base="https://github.com/vasyakrg/helm-tui/releases/download/v${VERSION}"
cat <<RB
class HelmTui < Formula
  desc "Terminal UI for browsing and managing Helm repositories"
  homepage "https://github.com/vasyakrg/helm-tui"
  license "MIT"

  depends_on "helm"

  on_macos do
    on_arm do
      url "${base}/helm-tui_${VERSION}_darwin_arm64.tar.gz"
      sha256 "$(sum darwin_arm64)"
    end
  end

  on_linux do
    on_arm do
      url "${base}/helm-tui_${VERSION}_linux_arm64.tar.gz"
      sha256 "$(sum linux_arm64)"
    end
    on_intel do
      url "${base}/helm-tui_${VERSION}_linux_amd64.tar.gz"
      sha256 "$(sum linux_amd64)"
    end
  end

  def install
    bin.install "helm-tui"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/helm-tui --version")
  end
end
RB
