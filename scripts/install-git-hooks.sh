#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
hooks_dir="$root/.git/hooks"

install -m 0755 "$root/scripts/hooks/pre-commit" "$hooks_dir/pre-commit"
echo "installed git hooks"
