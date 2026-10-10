#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOWORK=off
if (( $# == 0 )); then
  echo 'usage: ./run.sh v0.7.2 [v0.8.1 ...]' >&2
  exit 2
fi
versions=("$@")
sweep=${SWEEP:-0}
sweep_only=${SWEEP_ONLY:-0}
if [[ "$sweep_only" == 1 ]]; then sweep=1; fi
repo_root=$(cd ../.. && pwd)
sweep_probes=(gosqlite-steady-turso-default gosqlite-steady-mild gosqlite-steady-medium gosqlite-steady-eager gosqlite-steady-turso-default-arena2 gosqlite-steady-medium-arena2)
probes=(mvcc-growth wal-growth mvcc-growth-checkpoint mvcc-growth-1conn mvcc-retention mvcc-retention-checkpoint mvcc-retention-1conn mvcc-steady wal-steady mvcc-forced-gc mvcc-open-reader build vacuum-into cache-size gosqlite-steady)
if [[ "$sweep_only" == 1 ]]; then probes=(); fi
if [[ "$sweep" == 1 ]]; then probes+=("${sweep_probes[@]}"); fi
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
  local probe=$1 status=0 driver_probe=$1
  shift
  local -a process_env=() probe_args=()
  if [[ "$probe" == gosqlite-steady* ]]; then
    driver_probe=gosqlite-steady
    process_env=(env -u MALLOC_ARENA_MAX)
    if [[ "$probe" != gosqlite-steady ]]; then probe_args=(-label "${probe#gosqlite-steady-}"); fi
    if [[ "$probe" == *-arena2 ]]; then process_env=(env MALLOC_ARENA_MAX=2); fi
  fi
  echo "version=$version probe=$probe" >&2
  timeout --kill-after=10s 10m "${process_env[@]}" "$binary" -probe "$driver_probe" "${probe_args[@]}" -out "$result" "$@" > "$result/$probe.log" 2>&1 || status=$?
  if (( status != 0 )); then
    if ! has_summary "$probe"; then error_line "$probe" "exit_status=$status; see $probe.log"; fi
    echo "version=$version probe=$probe exit_status=$status" >&2
  elif ! has_summary "$probe"; then
    error_line "$probe" "process returned without a summary; see $probe.log"
  fi
}
for version in "${versions[@]}"; do
  result="$PWD/results/$version"
  if [[ "$sweep_only" == 1 ]]; then result+=/gosqlite-sweep; fi
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
    # The copied module is deeper than the source module: keep the local wrapper replace valid.
    go mod edit -replace="github.com/dnl-fm/go-sqlite=$repo_root" || exit $?
    # MVS selects at least the driver's version required by the root module.
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
    if [[ "$sweep_only" != 1 ]]; then
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
      run_probe gosqlite-steady -rows 600000 -trim -db "$result/work/gosqlite-steady.db"
    fi
    if [[ "$sweep" == 1 ]]; then
      run_probe gosqlite-steady-turso-default -rows 600000 -trim -mvcc-gc-threshold 16384 -mvcc-checkpoint-threshold 4120000 -db "$result/work/turso-default.db"
      run_probe gosqlite-steady-mild -rows 600000 -trim -mvcc-gc-threshold 4096 -mvcc-checkpoint-threshold 1048576 -db "$result/work/mild.db"
      run_probe gosqlite-steady-medium -rows 600000 -trim -mvcc-gc-threshold 1024 -mvcc-checkpoint-threshold 262144 -db "$result/work/medium.db"
      run_probe gosqlite-steady-eager -rows 600000 -trim -mvcc-gc-threshold 1 -mvcc-checkpoint-threshold 0 -db "$result/work/eager.db"
      run_probe gosqlite-steady-turso-default-arena2 -rows 600000 -trim -mvcc-gc-threshold 16384 -mvcc-checkpoint-threshold 4120000 -db "$result/work/turso-default-arena2.db"
      run_probe gosqlite-steady-medium-arena2 -rows 600000 -trim -mvcc-gc-threshold 1024 -mvcc-checkpoint-threshold 262144 -db "$result/work/medium-arena2.db"
    fi
  fi
  cleanup
  active_work=''
  cat "$result/summary.tsv"
done
comparison="$PWD/results/comparison.md"
if [[ "$sweep_only" == 1 ]]; then comparison="$PWD/results/gosqlite-sweep-comparison.md"; fi
summary_path() {
  local path="results/$1"
  if [[ "$sweep_only" == 1 ]]; then path+=/gosqlite-sweep; fi
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
        mvcc-steady|wal-steady|mvcc-forced-gc|mvcc-open-reader|gosqlite-steady*)
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
if [[ "$sweep" == 1 ]]; then
  sweep_comparison="$PWD/results/gosqlite-sweep-comparison.md"
  {
    printf '| Version | Label | Verdict | End RssAnon MiB | After trim MiB | Elapsed s | Inserts/s | Pragma readbacks and arena limit |\n'
    printf '|---|---|---|---:|---:|---:|---:|---|\n'
    for version in "${versions[@]}"; do
      awk -F '\t' -v version="$version" '
        $1 ~ /^gosqlite-steady-/ {
          delete m; readbacks=""; n=split($3, fields, " ")
          for (i=1; i<=n; i++) {
            split(fields[i], pair, "="); m[pair[1]]=pair[2]
            if (pair[1] ~ /^conn[0-9]+_/ || pair[1] == "malloc_arena_max") readbacks=readbacks " " fields[i]
          }
          label=$1; sub(/^gosqlite-steady-/, "", label)
          printf "| %s | %s | %s | %s | %s | %s | %s |%s |\n", version, label, $2, m["rss_end_mib"], m["rss_after_trim_mib"], m["elapsed_s"], m["inserts_per_s"], readbacks
        }
      ' "$(summary_path "$version")"
    done
  } > "$sweep_comparison"
  cat "$sweep_comparison"
else
  cat "$comparison"
fi
