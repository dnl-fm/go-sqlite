#!/usr/bin/env bash
set -euo pipefail

warn_limit=500
fail_limit=600

exceptions=()

is_exception() {
  local file="$1"
  local exception
  for exception in "${exceptions[@]}"; do
    if [[ "$file" == "$exception" ]]; then
      return 0
    fi
  done
  return 1
}

status=0
while IFS= read -r file; do
  if [[ ! -f "$file" ]]; then
    continue
  fi

  lines=$(wc -l < "$file")
  if is_exception "$file"; then
    if (( lines > fail_limit )); then
      printf 'WARN file-size exception %s has %d lines (limit %d)\n' "$file" "$lines" "$fail_limit"
    fi
    continue
  fi
  if (( lines > fail_limit )); then
    printf 'FAIL file-size %s has %d lines (limit %d)\n' "$file" "$lines" "$fail_limit"
    status=1
  elif (( lines > warn_limit )); then
    printf 'WARN file-size %s has %d lines (warn %d)\n' "$file" "$lines" "$warn_limit"
  fi
done < <(git ls-files '*.go' | grep -Ev '(^tmp/|generated|\.pb\.go$|_mock\.go$)')

exit "$status"
