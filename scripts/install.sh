#!/usr/bin/env bash
# install.sh — build kincode, ad-hoc codesign with a stable identifier,
# install into ~/.localkin/bin/. Mirrors the kinclaw kernel's install
# script so the family stays consistent.
#
# kincode itself doesn't currently use AX / Screen Recording (it's a
# coding agent, not a computer-use kernel), so re-auth pain is less
# acute than kinclaw's. But ad-hoc-signing + stable install path are
# still good hygiene — future kincode skills (e.g. an MCP server that
# touches AX) would inherit the signed identity automatically.
#
# Run with no args; safe to re-run.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="$HOME/.localkin/bin"
INSTALL_PATH="$INSTALL_DIR/kincode"
IDENTIFIER="dev.localkin.kincode"

echo "==> Building kincode..."
cd "$REPO_DIR"
go build -o kincode ./cmd/kincode/

echo "==> Ad-hoc codesigning (identifier: $IDENTIFIER)..."
# No --options=runtime: hardened runtime enables library validation,
# which rejects ad-hoc-signed dylibs because their synthetic Team IDs
# differ from the host's. We can't notarize ad-hoc anyway (needs $99
# Apple cert, deferred to M6) so hardened runtime gains nothing and
# breaks future dlopen-using code paths. Same fix applies in
# kinclaw/scripts/install.sh — see that file for the full rationale.
codesign --force --sign - --identifier "$IDENTIFIER" ./kincode

echo "==> Installing to $INSTALL_PATH..."
mkdir -p "$INSTALL_DIR"
cp ./kincode "$INSTALL_PATH"
codesign --force --sign - --identifier "$IDENTIFIER" "$INSTALL_PATH"

echo
echo "✓ Installed: $INSTALL_PATH"
echo "  $(file "$INSTALL_PATH" | sed 's|.*: ||')"
echo "  Signed: $(codesign -dv "$INSTALL_PATH" 2>&1 | grep '^Identifier=' | head -1)"
echo
echo "Next:"
echo "  - Relaunch KinClaw Mac — supervisor will spawn $INSTALL_PATH."
echo "  - Re-run this script after kincode source changes."
