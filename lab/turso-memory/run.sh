#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOWORK=off
if (( $# == 0 )); then
  echo 'usage: ./run.sh v0.7.2 [v0.8.1 ...]' >&2
  exit 2
fi
versions=("$@")
probes=(mvcc-growth wal-growth mvcc-growth-checkpoint mvcc-growth-1conn mvcc-retention mvcc-retention-checkpoint mvcc-retention-1conn mvcc-steady wal-steady mvcc-forced-gc mvcc-open-reader build vacuum-into cache-size)
# Version names are also directory names. Reject path traversal and shell metacharacters.
for version in "${versions[@]}"; do
  if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.+-][a-zA-Z0-9.+-]+)?$ ]]; then
    echo "invalid version: $version" >&2
    exit 2
  fi
done
active_work=''
cleanup() { if [[ -n "$active_work" ]]; then rm -rf -- "$active_work"; fi; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
error_line() {
  local probe=$1 message=$2
  printf '%s\tERROR\tmessage=%s\tprobe must complete successfully\n' "$probe" "${message//$'\n'/ }" >> "$result/summary.tsv"
}
has_summary() { awk -F '\t' -v p="$1" '$1 == p {found=1} END {exit !found}' "$result/summary.tsv"; }
run_probe() {
  local probe=$1 status=0
  shift
  echo "version=$version probe=$probe" >&2
  timeout --kill-after=10s 10m "$binary" -probe "$probe" -out "$result" "$@" > "$result/$probe.log" 2>&1 || status=$?
  if (( status != 0 )); then
    if ! has_summary "$probe"; then error_line "$probe" "exit_status=$status; see $probe.log"; fi
    echo "version=$version probe=$probe exit_status=$status" >&2
  elif ! has_summary "$probe"; then
    error_line "$probe" "process returned without a summary; see $probe.log"
  fi
}
for version in "${versions[@]}"; do
  result="$PWD/results/$version"
  mkdir -p "$result"
  # Replace only generated outputs for this version, so failed reruns cannot reuse old evidence.
  rm -f -- "$result"/*.csv "$result"/*.log "$result/summary.tsv" "$result/versions.txt"
  rm -rf -- "$result/src" "$result/work"
  mkdir -p "$result/src" "$result/work"
  active_work="$result/work"
  : > "$result/summary.tsv"
  cp ./*.go go.mod go.sum "$result/src/"
  binary="$result/src/turso-memory"
  setup_status=0
  (
    cd "$result/src" || exit $?
    go mod edit -droprequire=github.com/tursodatabase/turso-go-platform-libs || exit $?
    timeout --kill-after=10s 10m go get "turso.tech/database/tursogo@$version" || exit $?
    timeout --kill-after=10s 10m go mod tidy || exit $?
    go list -m turso.tech/database/tursogo github.com/tursodatabase/turso-go-platform-libs > "$result/versions.txt" || exit $?
    timeout --kill-after=10s 10m go build -o "$binary" .
  ) > "$result/setup.log" 2>&1 || setup_status=$?
  # The explicit status checks inside the setup shell are essential: bash disables
  # errexit in a compound command used on the left of ||.
  if (( setup_status != 0 )) || [[ ! -x "$binary" ]]; then
    for probe in "${probes[@]}"; do error_line "$probe" "version setup failed; see setup.log"; done
  else
    for probe in mvcc-growth wal-growth mvcc-growth-checkpoint mvcc-growth-1conn; do
      run_probe "$probe" -db "$result/work/$probe.db"
    done
    verdict_status=0
    timeout --kill-after=10s 1m "$binary" -probe verdict-growth -out "$result" > "$result/verdict-growth.log" 2>&1 || verdict_status=$?
    for probe in mvcc-retention mvcc-retention-checkpoint mvcc-retention-1conn; do
      if ! has_summary "$probe"; then error_line "$probe" "growth verdict failed (exit_status=$verdict_status); see verdict-growth.log"; fi
    done
    run_probe mvcc-steady -rows 600000 -trim -db "$result/work/mvcc-steady.db"
    run_probe wal-steady -rows 600000 -db "$result/work/wal-steady.db"
    run_probe mvcc-forced-gc -rows 600000 -trim -db "$result/work/mvcc-forced-gc.db"
    run_probe mvcc-open-reader -rows 600000 -db "$result/work/mvcc-open-reader.db"
    run_probe build -db "$result/work/docs.db"
    if awk -F '\t' '$1 == "build" && $2 != "ERROR" {ok=1} END {exit !ok}' "$result/summary.tsv"; then
      run_probe vacuum-into -trim -db "$result/work/docs.db"
      run_probe cache-size -db "$result/work/docs.db"
    else
      error_line vacuum-into 'fixture build failed'
      error_line cache-size 'fixture build failed'
    fi
  fi
  cleanup
  active_work=''
  cat "$result/summary.tsv"
done
comparison="$PWD/results/comparison.md"
summary_path() {
  local path="results/$1"
  printf '%s/summary.tsv' "$path"
}
{
  printf '| Probe |'
  for version in "${versions[@]}"; do printf ' %s |' "$version"; done
  printf '\n|---|'
  for version in "${versions[@]}"; do printf '%s' '---|'; done
  printf '\n'
  for probe in "${probes[@]}"; do
    printf '| %s |' "$probe"
    for version in "${versions[@]}"; do
      case "$probe" in
        mvcc-steady|wal-steady|mvcc-forced-gc|mvcc-open-reader)
          cell=$(awk -F '\t' -v p="$probe" '$1 == p {print $2 " " $3; exit}' "$(summary_path "$version")")
          ;;
        *)
          cell=$(awk -F '\t' -v p="$probe" '$1 == p {split($3,m," "); print $2 " " m[1]; exit}' "$(summary_path "$version")")
          ;;
      esac
      printf ' %s |' "${cell//|/\\|}"
    done
    printf '\n'
  done
} > "$comparison"
cat "$comparison"
