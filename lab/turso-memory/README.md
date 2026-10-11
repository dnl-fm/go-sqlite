# Turso native-memory lab

This standalone module evaluates the raw Turso driver. go-sqlite v0.8.0 uses
modernc SQLite, so the `gosqlite-*` probes and threshold sweep are removed.
The files in `results/` and `OUTCOME.md` remain historical evidence for the
Turso-based v0.7.x library. They do not describe v0.8.0 defaults.

MVCC writes retained about 1.2–1.3 KB per committed row version in production.
Memory kept rising, and a pool reopen did not release it. A previous 300k-row
lab measured about 400 MB with MVCC versus 19 MB with WAL.

`VACUUM INTO` caused a production OOM crashloop on Turso 0.7.2. One snapshot
of a 299 MB database retained about 1.1 GB. Five passes sometimes retained
5.4 GB. Forced Go garbage collection did not release that native memory.

`PRAGMA cache_size` was accepted, but each connection appeared to retain the
full database working set. Four connections could consume four database
copies instead of four small caches. The cache probe measures each connection
separately to distinguish shared storage from separate caches.

Run on Linux, with Go and network access:

```sh
make lab-memory TURSO_VERSIONS="v0.7.2 v0.8.1"
# Or, from this directory:
./run.sh v0.7.2 v0.8.1
```

The runner copies the lab module per version. It records both the Go driver
and native library versions. It has no dependency on the root module.
`versions.txt` records the resolved versions, rather than the requested label.
Every probe runs in a fresh process with
a ten-minute timeout. A probe error does not stop the remaining probes.
Large database files are removed after each version. A rerun replaces that
version's evidence. `results/comparison.md` covers the versions in that run.

Each probe writes CSV samples and a line in `results/<version>/summary.tsv`.
Logs include checkpoint results and errors. Samples contain Linux `RssAnon`
and Go `HeapInuse`, in KiB, after `runtime.GC()` and `debug.FreeOSMemory()`.
Anonymous RSS includes native allocations but is not a direct allocation
counter. `NOT_REPRODUCED` means this workload did not cross its threshold.
`ERROR` means the probe could not complete. `build` reports `NOT_REPRODUCED`
when its fixture succeeds, because it tests no memory defect.

| Summary line | REPRODUCED threshold | Reason |
|---|---|---|
| `mvcc-retention` | At least 400 retained bytes/row AND final MVCC RSS at least 4× WAL RSS | Requires a sustained slope and a large gap from the control. |
| `mvcc-retention-checkpoint` | Same thresholds, against WAL | Tests whether explicit checkpoints release versions. |
| `mvcc-retention-1conn` | Same thresholds, against WAL | Tests whether idle connections retain versions. |
| `vacuum-into` | Final RSS increase / database bytes ≥ 1.0 | One retained database copy is already material. |
| `cache-size` | RSS increase > connections × cache KiB / 1024 + 64 MiB | Allows 64 MiB for non-cache overhead. |

The four raw growth lines report bytes/row and final RSS. Their individual
verdict uses only the 400-byte slope threshold. The `mvcc-retention*` lines
apply the complete comparison with WAL. The slope uses the exact halfway
sample at the default 300k rows. For odd tick totals, it uses the preceding
complete tick. `rows_half` records that choice.
Verdicts use raw RSS and row counts, before rounding the displayed metrics.

The `mvcc-steady` and `wal-steady` probes use 600k sample rows and one reserved
connection. Cleanup runs every 250 ticks with a 500-tick retention window.
The final live sample count is about 20k, and the probe requires actual
deletions. Security events still accumulate, as in the reference workload.
These probes report `GROWS` at 400 bytes/row over the second half, otherwise
`FLAT`. The summary records live samples, deleted samples, and MVCC pragma
readbacks. The comparison table includes every metric for the four new probes.

`mvcc-forced-gc` uses the steady workload with `mvcc_checkpoint_threshold=0`
and `mvcc_gc_threshold=1`. It verifies both readbacks before any schema writes.
`mvcc-open-reader` holds a second connection's cursor after exactly one
`Next()` at tick 100. It closes the cursor after the main workload, writes
250 more ticks, and records `rss_after_release_mib`.

The growth workload matches `~/werk/lab/turso-mvcc-memory/main.go`: 40 samples
and one security event per committed transaction, with a samples index.
Cleanup runs every 2,000 ticks and deletes ticks older than `tick - 20,000`
in batches of 5,000 rows. At the default 7,500 ticks, **nothing expires**.
`deleted_samples` makes this limitation explicit. Longer runs exercise actual
deletions but can exceed the intended memory budget.

All growth connections use `busy_timeout=5000`, `synchronous=NORMAL`, and
`cache_size=-64000`, as in the reference configuration. Journal mode is set
and verified on every connection. Plain growth pins four initialized
connections and writes through one. Three remain idle. The one-connection
variant has no idle peers. Checkpoints run every 20k rows before the sample.
A checkpoint error is recorded data, not a failed growth probe.

The fixture uses indexed lookup keys and deterministic, varied 2 KiB text
bodies. Batches of 1,000 rows reach approximately 150 MiB. The final closed
file size is recorded and used for ratios. Build memory cannot contaminate
the read probes because the build process exits first.

The vacuum probe uses one connection, a warm `count(*)`, and five passes.
Each target directory is removed before the memory sample. The cache probe
pins four connections and sets `cache_size=-2048` on each. It records the
readback, then scans both bodies and the lookup index on each connection.
All connections stay open through the final sample. Its summary includes
incremental and cumulative deltas after each connection.

The runner adds `-trim` to `mvcc-steady`, `mvcc-forced-gc`, and `vacuum-into`.
This optional flag calls glibc `malloc_trim(0)` after the ordinary end sample.
VACUUM records an ordinary sample and a trim sample after every pass, including
the last pass. `rss_end_mib` and `rss_after_trim_mib` describe the final pair.
The VACUUM verdict still uses the ordinary final pass and the same ratio
threshold. Trimming between passes can change later pass RSS, so this sequence
differs from the round-one run without trimming.

Both checked Linux amd64 native libraries use glibc allocation functions.
`nm -D` showed imports for `calloc`, `free`, `malloc`, `posix_memalign`, and
`realloc` at `GLIBC_2.2.5`, with no exported mimalloc or jemalloc symbols.
The artifacts are `github.com/tursodatabase/turso-go-platform-libs@v0.7.2`
and `@v0.8.1`, under `libs/linux_amd64/libturso_sync_sdk_kit.so` in the Go
module cache. The v0.8.1 source confirms no allocator replacement in
`sync/sdk-kit` or `sdk-kit`. Its `sync/sdk-kit/Cargo.toml` defines only the
`pure-rust-crypto` feature. `core/Cargo.toml` lists mimalloc as a dev
dependency for benchmarks. The Rust binding uses mimalloc in a separate binary.

A large RSS decrease after trim identifies allocator retention. No decrease
does not prove a live leak: glibc can retain freed memory that it cannot return.
The CSV records the `malloc_trim` return value. Linux systems without glibc
report a probe error when `-trim` cannot load `libc.so.6` or its trim symbol.

For a bounded smoke check, build the binary and use its workload flags:

```sh
GOWORK=off go vet ./...
GOWORK=off go test ./...
GOWORK=off go build -o /tmp/turso-memory .
/tmp/turso-memory -probe mvcc-growth -rows 20000 -db /tmp/memory-smoke.db -out /tmp/memory-smoke
```

Other flags are `-probe`, `-db`, `-size-mb`, `-passes`, `-conns`, `-cache-kib`,
`-trim`, and `-out`. The steady, GC, and reader probes default to 600k rows.
Round-one growth probes keep their 300k-row default. An explicit `-rows` overrides either default.
Each database path must be fresh, except the shared closed fixture
for `vacuum-into` and `cache-size`. Small smoke runs cannot establish the
production thresholds.
