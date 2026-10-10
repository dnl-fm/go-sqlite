# sqlite-writers: full run 2026-10-08

- started 2026-10-08T14:34:04+02:00, ended 2026-10-08T14:43:20+02:00 (9m16s wall clock), test exit code 0
- go1.26.5, linux/amd64, GOMAXPROCS 16, modernc.org/sqlite v1.60.1, SQLite 3.53.4
- 16 writers, 8 readers pausing 1ms between reads; inserts 10000/writer (FULL rows 500/writer), counter 5000/writer, long-tx 10s with 8 inserters behind a 100ms transaction
- other labs were benchmarking on the same machine at the same time; treat the numbers as ratios, not absolutes

Latencies in milliseconds. `lost` = acknowledged writes missing from the database. `wal MB` = size of the `-wal` file when the writers finished.

| workload | strategy | ops | busy | other | lost | ops/s | write p50 | write p99 | write max | read p50 | read p99 | wal MB |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| inserts | A naive | 160000 | 0 | 0 | 0 | 19412 | 0.30 | 6.14 | 629.13 | 0.99 | 8.70 | 1384.1 |
| inserts | B immediate | 160000 | 0 | 0 | 0 | 20351 | 0.28 | 5.81 | 1129.55 | 0.93 | 7.85 | 1383.0 |
| inserts | C split | 160000 | 0 | 0 | 0 | 7421 | 0.51 | 34.58 | 168.29 | 0.26 | 6.54 | 4.9 |
| inserts | D queue | 160000 | 0 | 0 | 0 | 6727 | 1.69 | 14.68 | 35.27 | 0.29 | 6.05 | 6.2 |
| inserts | E multi-process | 160000 | 0 | 0 | 0 | 30857 | 0.08 | 7.34 | 2444.75 | 0.26 | 1.76 | 782.4 |
| inserts | C split (FULL) | 8000 | 0 | 0 | 0 | 160 | 68.55 | 422.24 | 990.19 | 0.02 | 0.10 | 3.9 |
| inserts | D queue (FULL) | 8000 | 0 | 0 | 0 | 2008 | 7.66 | 14.67 | 26.30 | 0.02 | 0.10 | 4.0 |
| counter | A naive | 34252 | 45748 | 0 | 0 | 30218 | 0.16 | 0.93 | 20.26 | 0.33 | 1.25 | 134.2 |
| counter | B immediate | 80000 | 0 | 0 | 0 | 39050 | 0.19 | 1.91 | 429.04 | 0.47 | 2.48 | 311.3 |
| counter | C split | 80000 | 0 | 0 | 0 | 25158 | 0.23 | 20.07 | 33.54 | 0.02 | 0.12 | 3.9 |
| counter | D queue | 80000 | 0 | 0 | 0 | 9090 | 1.98 | 2.30 | 43.13 | 0.02 | 0.07 | 3.9 |
| counter | E multi-process | 80000 | 0 | 0 | 0 | 36278 | 0.07 | 6.71 | 929.48 | 0.03 | 0.08 | 203.1 |
| long-tx | A naive | 556 | 4 | 0 | 0 | 55 | 100.49 | 604.95 | 5302.61 | 299.38 | 903.56 | 3.9 |
| long-tx | B immediate | 645 | 6 | 0 | 0 | 64 | 100.37 | 516.36 | 805.90 | 198.89 | 804.71 | 3.9 |
| long-tx | C split | 825 | 0 | 0 | 0 | 82 | 100.48 | 503.50 | 805.25 | 0.01 | 0.04 | 3.9 |
| long-tx | D queue | 891 | 0 | 0 | 0 | 88 | 101.78 | 109.09 | 109.13 | 0.01 | 0.04 | 1.3 |
| long-tx | E multi-process | 1996 | 4 | 0 | 0 | 198 | 0.05 | 294.91 | 9535.27 | 0.01 | 0.04 | 10.0 |

counter / A naive, first of 45748 busy errors: `write: database is locked (5) (SQLITE_BUSY)`

long-tx / A naive, first of 4 busy errors: `write: database is locked (5) (SQLITE_BUSY)`

long-tx / B immediate, first of 6 busy errors: `write: database is locked (5) (SQLITE_BUSY)`

long-tx / E multi-process, first of 4 busy errors: `write: database is locked (5) (SQLITE_BUSY)`

## wal-growth

1m0s per variant, 4 writers pausing 2ms between commits, 8 readers. Sizes in MB.

| variant | writes/s | errors | write p99 ms | read p99 ms | wal @10s | @20s | @30s | @40s | @50s | wal max | wal at end | after TRUNCATE | notes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| C split | 1576 | 0 | 15.03 | 1.87 | 4.0 | 4.0 | 4.0 | 4.0 | 4.0 | 4.0 | 4.0 | 0.0 |  |
| C split, readers never pause | 1045 | 0 | 32.53 | 0.55 | 4.0 | 4.4 | 5.0 | 6.6 | 7.7 | 9.2 | 9.2 | 0.0 |  |
| C split, one read tx held open | 1779 | 0 | 0.49 | 4.76 | 152.3 | 307.3 | 463.2 | 616.4 | 768.8 | 919.3 | 919.3 | 0.0 | checkpoint with the read tx still open: busy=true, wal still 919.3 MB |
| B immediate (4 writer conns) | 1207 | 0 | 10.84 | 1.34 | 99.4 | 202.7 | 308.5 | 414.8 | 520.0 | 623.9 | 623.9 | 0.0 |  |
| B immediate + TRUNCATE checkpoint every 5s | 1205 | 0 | 10.86 | 1.35 | 99.9 | 203.0 | 306.9 | 52.9 | 158.1 | 359.7 | 262.1 | 0.0 | 12 timed checkpoints, 11 busy |
| B immediate, wal_autocheckpoint=0 + TRUNCATE checkpoint every 5s | 1448 | 0 | 3.28 | 2.81 | 61.5 | 64.7 | 66.4 | 64.3 | 60.5 | 66.4 | 55.4 | 0.0 | 12 timed checkpoints, 0 busy |

## wal-matrix

Run of 2026-10-08T14:07:34+02:00, go1.26.5, GOMAXPROCS 16, modernc.org/sqlite v1.60.1, SQLite 3.53.4. 4 writer goroutines offered 800 commits/s of 2048-byte rows for 30s per cell; 8 readers on their own read-only pool of 8, every read one `Store.Read` transaction. Timed checkpoints run every 5s; `Store.Checkpoint` gets a quiet window of 50ms unless the cell names another. The claim cells run their build's own load instead: see `matrixCells`. Sizes in MB, latencies in ms.

`restarts` counts how often the WAL started over from its first frame: the increase of salt-1 in the wal-index header, which SQLite bumps by one per restart. `not checkpointed` is the median, over the per-second samples, of the WAL that no checkpoint had copied into the database yet: near zero beside a large WAL means checkpoints finish and the WAL still cannot restart, near the WAL size means they cannot finish. `open reads` is the mean number of reads in flight.

### Writer connections x reader pattern

| cell | commits | errors | write p99 | write max | reads | read p99 | read max | open reads | wal @10s | @20s | wal max | wal end | restarts | not checkpointed | timed checkpoints |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| C 1 writer conn, readers none | 22350 | 0 | 23.86 | 41.36 | 0 | 0.00 | 0.00 | 0.00 | 3.9 | 4.0 | 4.0 | 4.0 | 93 | 1.9 |  |
| B 4 writer conns, readers none | 23997 | 0 | 5.26 | 16.93 | 0 | 0.00 | 0.00 | 0.00 | 131.1 | 263.0 | 395.1 | 395.1 | 0 | 0.1 |  |
| C 1 writer conn, readers point | 22316 | 0 | 23.77 | 49.16 | 207143 | 0.10 | 0.35 | 0.23 | 4.0 | 4.0 | 4.0 | 4.0 | 93 | 2.5 |  |
| B 4 writer conns, readers point | 23996 | 0 | 5.25 | 16.75 | 206714 | 0.12 | 5.39 | 0.26 | 130.9 | 262.7 | 394.6 | 394.6 | 0 | 0.1 |  |
| C 1 writer conn, readers scan | 22149 | 0 | 24.20 | 53.38 | 162763 | 0.55 | 1.13 | 0.97 | 4.0 | 4.0 | 4.0 | 4.0 | 92 | 1.8 |  |
| B 4 writer conns, readers scan | 23997 | 0 | 5.46 | 22.43 | 160850 | 0.63 | 5.53 | 1.16 | 130.9 | 262.7 | 394.6 | 394.6 | 0 | 0.1 |  |
| C 1 writer conn, readers longscan | 21652 | 0 | 12.53 | 61.34 | 11565 | 60.95 | 66.67 | 7.54 | 100.5 | 222.0 | 348.7 | 348.7 | 2 | 0.4 |  |
| B 4 writer conns, readers longscan | 23927 | 0 | 9.81 | 113.10 | 9054 | 81.07 | 87.39 | 7.64 | 130.3 | 261.9 | 393.7 | 393.7 | 0 | 0.4 |  |
| C 1 writer conn, readers overlap | 21082 | 0 | 25.67 | 84.18 | 14510914 | 0.08 | 6.95 | 7.97 | 4.2 | 4.6 | 4.7 | 4.7 | 86 | 1.8 |  |
| B 4 writer conns, readers overlap | 23987 | 0 | 5.29 | 36.46 | 14447981 | 0.08 | 8.69 | 7.97 | 130.8 | 262.4 | 394.2 | 394.2 | 0 | 0.1 |  |
| C 1 writer conn, readers pinned | 23935 | 0 | 0.10 | 23.00 | 0 | 0.00 | 0.00 | 0.00 | 130.8 | 262.7 | 394.5 | 394.5 | 0 | 209.8 |  |
| B 4 writer conns, readers pinned | 23967 | 0 | 0.10 | 33.46 | 0 | 0.00 | 0.00 | 0.00 | 131.1 | 263.1 | 395.0 | 395.0 | 0 | 210.2 |  |

### Mitigations

| cell | commits | errors | write p99 | write max | reads | read p99 | read max | open reads | wal @10s | @20s | wal max | wal end | restarts | not checkpointed | timed checkpoints |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| B 4 writer conns, readers point + journal_size_limit=16MiB | 23997 | 0 | 5.26 | 32.71 | 206711 | 0.12 | 5.36 | 0.27 | 131.1 | 263.2 | 395.4 | 395.4 | 0 | 0.1 |  |
| B 4 writer conns, readers point + no auto-checkpoint, RESTART every 5s | 23713 | 0 | 0.14 | 128.65 | 205014 | 0.12 | 0.53 | 0.27 | 65.2 | 65.2 | 65.2 | 65.2 | 5 | 38.6 | 6, 0 busy, slowest 79.43 ms |
| B 4 writer conns, readers point + no auto-checkpoint, TRUNCATE every 5s | 23654 | 0 | 0.15 | 129.65 | 205412 | 0.12 | 1.86 | 0.27 | 65.0 | 65.0 | 65.0 | 64.9 | 6 | 38.5 | 6, 0 busy, slowest 100.42 ms |
| B 4 writer conns, readers point + Store.Checkpoint every 5s | 23916 | 0 | 5.28 | 103.59 | 205559 | 0.12 | 65.71 | 0.31 | 130.9 | 65.0 | 196.9 | 131.5 | 3 | 0.1 | 6, 3 busy, slowest 65.62 ms |
| B 4 writer conns, readers point + no auto-checkpoint, Store.Checkpoint every 5s | 23639 | 0 | 0.15 | 128.64 | 201745 | 0.12 | 99.63 | 0.41 | 64.9 | 64.6 | 65.0 | 64.8 | 6 | 38.3 | 6, 0 busy, slowest 99.60 ms |
| C 1 writer conn, readers longscan + journal_size_limit=16MiB | 21920 | 0 | 11.87 | 46.66 | 10830 | 63.42 | 68.68 | 7.56 | 103.4 | 226.0 | 353.0 | 353.0 | 2 | 0.4 |  |
| C 1 writer conn, readers longscan + no auto-checkpoint, RESTART every 5s | 19506 | 0 | 3.39 | 5128.81 | 10876 | 70.86 | 78.73 | 7.54 | 64.9 | 64.9 | 64.9 | 64.9 | 4 | 50.3 | 6, 1 busy, slowest 5077.00 ms |
| C 1 writer conn, readers longscan + no auto-checkpoint, TRUNCATE every 5s | 23238 | 0 | 3.35 | 290.27 | 9283 | 75.95 | 91.09 | 7.61 | 64.1 | 63.5 | 64.9 | 62.2 | 6 | 37.8 | 6, 0 busy, slowest 280.96 ms |
| C 1 writer conn, readers longscan + Store.Checkpoint every 5s | 17901 | 0 | 26.87 | 100.58 | 15339 | 45.24 | 109.99 | 7.41 | 31.8 | 56.6 | 60.2 | 60.2 | 12 | 0.2 | 6, 0 busy, slowest 58.16 ms |
| C 1 writer conn, readers longscan + no auto-checkpoint, Store.Checkpoint every 5s | 23648 | 0 | 3.42 | 99.78 | 8708 | 84.06 | 172.69 | 7.64 | 64.7 | 64.2 | 64.8 | 64.3 | 6 | 38.3 | 6, 0 busy, slowest 140.95 ms |
| C 1 writer conn, readers pinned + journal_size_limit=16MiB | 23935 | 0 | 0.10 | 23.19 | 0 | 0.00 | 0.00 | 0.00 | 130.7 | 261.8 | 393.4 | 393.4 | 0 | 209.3 |  |
| C 1 writer conn, readers pinned + no auto-checkpoint, RESTART every 5s | 4013 | 0 | 0.10 | 20026.93 | 0 | 0.00 | 0.00 | 0.00 | 64.9 | 64.9 | 65.1 | 65.1 | 0 | 65.0 | 12, 12 busy, slowest 5067.07 ms |
| C 1 writer conn, readers pinned + no auto-checkpoint, TRUNCATE every 5s | 4015 | 0 | 0.10 | 15086.59 | 0 | 0.00 | 0.00 | 0.00 | 64.9 | 64.9 | 65.1 | 65.1 | 0 | 64.9 | 6, 6 busy, slowest 5072.83 ms |
| C 1 writer conn, readers pinned + Store.Checkpoint every 5s | 23759 | 0 | 0.10 | 51.10 | 0 | 0.00 | 0.00 | 0.00 | 130.0 | 260.2 | 390.7 | 390.7 | 0 | 207.4 | 6, 6 busy, slowest 50.84 ms |
| C 1 writer conn, readers pinned + no auto-checkpoint, Store.Checkpoint every 5s | 23748 | 0 | 0.10 | 96.28 | 0 | 0.00 | 0.00 | 0.00 | 129.7 | 259.8 | 390.2 | 390.2 | 0 | 207.2 | 6, 6 busy, slowest 98.26 ms |
| C 1 writer conn, readers longscan + Store.Checkpoint every 5s, 5ms window | 21496 | 0 | 15.13 | 69.17 | 12371 | 61.03 | 79.22 | 7.49 | 56.2 | 124.2 | 251.3 | 251.3 | 5 | 0.4 | 6, 4 busy, slowest 43.30 ms |

### The original claims

| cell | commits | errors | write p99 | write max | reads | read p99 | read max | open reads | wal @10s | @20s | wal max | wal end | restarts | not checkpointed | timed checkpoints |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| this lab: B immediate, inserts burst | 160000 | 0 | 5.88 | 731.70 | 24456 | 8.29 | 21.03 | 4.81 | | | 1385.9 | 1385.9 | 1 | 1.5 |  |
| other build: C split, scan readers | 47611 | 0 | 22.84 | 49.49 | 321543 | 0.85 | 3.25 | 1.29 | 4.0 | 4.0 | 4.1 | 4.1 | 199 | 2.1 |  |
| other build: C split, scan readers, count over the table | 44433 | 0 | 11.24 | 69.26 | 15771 | 122.97 | 140.05 | 7.70 | 75.3 | 188.3 | 723.1 | 723.1 | 2 | 0.6 |  |
| other build: B immediate on one shared pool, count over the table | 12758 | 0 | 71.74 | 217.48 | 27847 | 73.36 | 226.63 | 7.43 | 66.6 | 105.5 | 209.3 | 209.3 | 0 | 0.1 |  |
| other build: B immediate on one shared pool, count over the table, synchronous=OFF | 14783 | 0 | 60.41 | 132.49 | 25799 | 63.33 | 142.58 | 7.48 | 75.1 | 100.3 | 100.3 | 100.3 | 11 | 0.0 |  |
## load

Run of 2026-10-10T12:11:08+02:00, go1.26.5, GOMAXPROCS 16, modernc.org/sqlite v1.60.1, SQLite 3.53.4. Real ext4 `.labdata/`, 90% 512-byte inserts, 10% read-modify-write over 64 hot rows; eight `Store.Read` point readers pausing 1ms. Calibration uses 64 closed-loop submitters for 10s; capacity counts acknowledgements inside that interval, excluding drain. Open-loop write latency includes generator lag. Histograms are bounded and round percentiles up to 100us. If p99 reaches the 1s overflow bin, it reports the maximum as a conservative bound.

Capacity is acknowledged requests/s; transaction throughput counts distinct committed transactions (D groups requests). Commit gaps approximate commit times from the first successful acknowledgement of each transaction, including active-window start/end gaps; they are not gaps between individual grouped replies. At scenario start, 200 fresh-file 4KiB appends followed by `fdatasync` on the same ext4 filesystem measure sync p50/p99/max; full 50% sustained write p99 must be <=3 times that measured p99, write max <=250ms and reader p99 <=10ms for C-NORMAL, D-NORMAL and D-FULL. At 80%, latency is recorded while zero rejections, correctness and commit gaps <=250ms remain asserted. C-FULL sustained tails/deadlines are recorded only. Both bursts use 50% background and a 200% total spike. In full mode the spike starts at 1s, lasts 2s, and leaves 10s of recovery in a 13s cell. The added work above background is 1.5 times capacity for 2s, or 3s of capacity's work; 50% spare needs about 6s to drain it. The burst carries outer and inner 5s deadlines: zero rejections, write max <=5s, and observed recovery within 8s after spike end, before the cell ends. The identical burst-short-deadline uses 1s and permits only typed `context.DeadlineExceeded` rejections returning within 1050ms. Other requests carry an outer 1s deadline; overload adds a child admission deadline at 100ms, with typed rejected completions within 150ms. Short mode asserts correctness, zero unexpected errors, commit gaps (race-only 10x allowance), and fast typed rejections; latency/drain/plateau are recorded. Full mode additionally asserts 50% sustained latency, burst latency/recovery, and overload plateau. Compact calibration/sustained/burst/spike/overload durations are 0.5/1.5/1/0.2/1.5s; the spike starts at 0.1s, leaving 0.7s recovery. Memory samples every 200ms; plateau ignores the first second, compares equal early/late windows: late maximum heap <= early maximum *1.5 +16MiB, late maximum goroutines <= early maximum +max(64,25%). Three equal post-warm windows compare low-water marks. Monotonic growth across all three windows fails when total growth exceeds max(2MiB,10%) for heap or max(16,10%) for goroutines. Bounded oscillation remains subject to the peak caps. These finite-run tolerances do not prove absence of smaller leaks. Drain is measured from spike end until two complete post-spike arrival windows have write p99 <= the disk-relative sustained bound. Its observation time is the later of both windows closing and their last completion; both must occur before measurement ends. Windows are 250ms in full mode, 100ms in compact mode; -1ns means no observed drain. Every cell verdict includes the measured disk stats and derived bound and uses that cell's asserted mode.

| pattern | phase | capacity req/s | capacity tx/s | offered req/s | accepted | rejected | errors | lost | write p99 ms | write max ms | reject max ms | read p99 ms | gap ms | drain | plateau | verdict |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---|---|
| C-NORMAL | calibration | 11299 | 11299 | 0 | 113050 | 0 | 0 | 0 | 44.60 | 88.62 | 0.00 | 0.10 | 56.13 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-NORMAL | 50% | 11299 | 11299 | 5649 | 338959 | 0 | 0 | 0 | 35.90 | 94.66 | 0.00 | 0.10 | 40.40 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-NORMAL | 80% | 11299 | 11299 | 9039 | 542277 | 57 | 0 | 0 | 244.30 | 999.68 | 1001.06 | 0.10 | 48.00 | -1ns | true | RED: sustained deadline errors: 57 (full contract; 80% sustained latency recorded; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-NORMAL | burst | 11299 | 11299 | 5649 | 105667 | 1668 | 0 | 0 | 4999.67 | 4999.67 | 5001.14 | 0.10 | 46.34 | 6.000726731s | true | RED: burst deadline errors: 1668 (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-NORMAL | burst-short-deadline | 11299 | 11299 | 5649 | 89425 | 17910 | 0 | 0 | 957.00 | 999.98 | 1003.75 | 0.10 | 24.98 | 2.00291125s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-NORMAL | overload | 11299 | 11299 | 16948 | 228499 | 110462 | 0 | 0 | 98.30 | 123.97 | 101.86 | 0.10 | 51.72 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | calibration | 76941 | 1202 | 0 | 769471 | 0 | 0 | 0 | 17.70 | 47.74 | 0.00 | 0.10 | 47.73 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | 50% | 76941 | 1202 | 38470 | 2308225 | 0 | 0 | 0 | 26.90 | 59.54 | 0.00 | 0.10 | 48.54 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | 80% | 76941 | 1202 | 61553 | 3679382 | 13835 | 0 | 0 | 988.60 | 1076.02 | 1048.01 | 0.10 | 72.02 | -1ns | false | RED: sustained deadline errors: 13835 (full contract; 80% sustained latency recorded; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | burst | 76941 | 1202 | 38470 | 730961 | 0 | 0 | 0 | 2884.84 | 2884.84 | 0.00 | 0.10 | 27.54 | 6.511249453s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | burst-short-deadline | 76941 | 1202 | 38470 | 610055 | 120906 | 0 | 0 | 1041.07 | 1041.07 | 1011.83 | 0.10 | 52.05 | 2.502267596s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-NORMAL | overload | 76941 | 1202 | 115411 | 1412342 | 896061 | 0 | 0 | 114.10 | 146.71 | 107.15 | 0.10 | 47.88 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | calibration | 159 | 159 | 0 | 1657 | 0 | 0 | 0 | 3051.45 | 3051.45 | 0.00 | 0.10 | 45.60 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | 50% | 159 | 159 | 80 | 4780 | 0 | 0 | 0 | 12.80 | 32.97 | 0.00 | 0.10 | 38.77 | -1ns | true | PASS (full contract; C-FULL sustained tails/deadlines recorded; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | 80% | 159 | 159 | 127 | 7647 | 0 | 0 | 0 | 76.50 | 442.32 | 0.00 | 0.10 | 46.04 | -1ns | true | PASS (full contract; C-FULL sustained tails/deadlines recorded; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | burst | 159 | 159 | 80 | 1509 | 4 | 0 | 0 | 4986.94 | 4986.94 | 5000.87 | 0.10 | 97.19 | 5.255987091s | true | RED: burst deadline errors: 4 (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | burst-short-deadline | 159 | 159 | 80 | 1260 | 253 | 0 | 0 | 956.80 | 1001.82 | 1001.08 | 0.10 | 31.66 | 2.004104798s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| C-FULL | overload | 159 | 159 | 239 | 2199 | 2581 | 0 | 0 | 109.40 | 173.50 | 101.13 | 0.30 | 78.02 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | calibration | 7897 | 123 | 0 | 79037 | 0 | 0 | 0 | 28.30 | 45.28 | 0.00 | 0.10 | 45.27 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | 50% | 7897 | 123 | 3949 | 236920 | 0 | 0 | 0 | 36.90 | 85.46 | 0.00 | 0.10 | 41.99 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | 80% | 7897 | 123 | 6318 | 379073 | 0 | 0 | 0 | 28.00 | 47.07 | 0.00 | 0.10 | 29.25 | -1ns | true | PASS (full contract; 80% sustained latency recorded; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | burst | 7897 | 123 | 3949 | 75025 | 0 | 0 | 0 | 1607.94 | 1607.94 | 0.00 | 0.10 | 35.16 | 4.027778435s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | burst-short-deadline | 7897 | 123 | 3949 | 68290 | 6735 | 0 | 0 | 1019.72 | 1019.72 | 1001.20 | 0.10 | 28.89 | 2.264889822s | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
| D-FULL | overload | 7897 | 123 | 11846 | 91128 | 145792 | 0 | 0 | 117.60 | 127.69 | 103.95 | 0.10 | 27.74 | -1ns | true | PASS (full contract; sync p50/p99/max=6.10/17.93/23.77 ms; sustained bound=53.80 ms) |
