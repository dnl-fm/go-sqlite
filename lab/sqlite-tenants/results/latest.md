# sqlite-tenants: full run 2026-10-07

## run

| metric | value | unit |
|---|---:|---|
| start | 2026-10-07T19:27:07+02:00 |  |
| end | 2026-10-07T19:30:44+02:00 |  |
| wall | 217.8 | s |
| go | go1.26.5 |  |
| modernc.org/sqlite | v1.60.1 |  |
| cpus | 16 |  |
| race detector | false |  |

## 03-lru

| metric | value | unit |
|---|---:|---|
| max open (configured) | 8 | tenants |
| peak open (manager counter) | 8 | tenants |
| peak open (distinct .db in /proc/self/fd) | 8 | tenants |
| kernel samples taken | 11247 |  |
| evictions | 1791 |  |
| failed operations | 0 |  |

## 04-scaling

| metric | value | unit |
|---|---:|---|
| writes per run (64 writers) | 32000 | writes |
| 16 tenants x 4 writers | 122364.84 | writes/s |
| 1 database x 64 writers | 19926.14 | writes/s |
| speedup | 6.14 | x |

## 05-noisy

| metric | value | unit |
|---|---:|---|
| neighbor write transaction | 100.00 | ms |
| other tenants write p50 | 0.02 | ms |
| other tenants write p99 | 0.09 | ms |
| other tenants write max | 0.18 | ms |
| same tenant write max | 100.33 | ms |

## 07-backup

| metric | value | unit |
|---|---:|---|
| backup 0 under 4 writers | 34.08 | ms |
| backup 1 under 4 writers | 46.45 | ms |
| backup 2 under 4 writers | 29.41 | ms |
| acme-0 rows (live before / copy / live after) | 2676 / 2679 / 3123 | rows |
| acme-1 rows (live before / copy / live after) | 3129 / 3129 / 3681 | rows |
| acme-2 rows (live before / copy / live after) | 4389 / 4392 / 4764 | rows |

## 08-crash

| metric | value | unit |
|---|---:|---|
| commits acknowledged before SIGKILL | 300 | tx |
| transactions present after reopen | 300 | tx |
| WAL size at reopen | 1330792 | bytes |

## 09-attach

| metric | value | unit |
|---|---:|---|
| databases attached before the limit | 10 | databases |
| error on the next ATTACH | SQL logic error: too many attached databases - max 10 (1) |  |

## 10-create

| metric | value | unit |
|---|---:|---|
| tenants created | 1000 | tenants |
| wall, 16 creators | 5.58 | s |
| create p50 | 90.22 | ms |
| create p99 | 100.53 | ms |
| disk, one open empty tenant: .db | 4096 | bytes |
| disk, one open empty tenant: .db-wal | 8272 | bytes |
| disk, one open empty tenant: .db-shm | 32768 | bytes |
| disk per empty tenant, closed: file size | 8192 | bytes |
| disk per empty tenant, closed: allocated | 8192 | bytes |

## 10-fds

| metric | value | unit |
|---|---:|---|
| fds, tenant open, writer only | 3 | fds |
| fds, writer + 4 busy readers (peak) | 11 | fds |
| fds, after the readers go idle | 9 | fds |

## 10-migrate

| metric | value | unit |
|---|---:|---|
| MigrateAll, 1 step, concurrency 1: wall | 23.06 | s |
| MigrateAll, 1 step, concurrency 1: rate | 43.37 | tenants/s |
| MigrateAll, 1 step, concurrency 8: wall | 7.22 | s |
| MigrateAll, 1 step, concurrency 8: rate | 138.52 | tenants/s |
| MigrateAll, 1 step, concurrency 32: wall | 2.06 | s |
| MigrateAll, 1 step, concurrency 32: rate | 485.28 | tenants/s |
| MigrateAll, nothing to do, concurrency 32: wall | 0.14 | s |

## 10-open-10

| metric | value | unit |
|---|---:|---|
| tenants open at once | 10 | tenants |
| RssAnon, nothing open | 2580 | kB |
| RssAnon, all open, each read once | 5184 | kB |
| RssAnon per open tenant, each read once | 260.40 | kB |
| fds per open tenant, each read once | 5.00 | fds |
| cold open + first query, cache has room p50 | 0.45 | ms |
| cold open + first query, cache has room p99 | 0.71 | ms |
| cold open + first query, evicting a read-only tenant p50 | 0.40 | ms |
| cold open + first query, evicting a read-only tenant p99 | 0.55 | ms |
| RssAnon, all open, each written once | 7236 | kB |
| RssAnon per open tenant, each written once | 465.60 | kB |
| fds per open tenant, each written once | 5.00 | fds |
| warm query p50 | 0.01 | ms |
| warm query p99 | 0.02 | ms |
| cold open + first query, evicting a written tenant p50 | 10.20 | ms |
| cold open + first query, evicting a written tenant p99 | 21.46 | ms |
| RssAnon, after Close | 7100 | kB |

## 10-open-100

| metric | value | unit |
|---|---:|---|
| tenants open at once | 100 | tenants |
| RssAnon, nothing open | 2632 | kB |
| RssAnon, all open, each read once | 26392 | kB |
| RssAnon per open tenant, each read once | 237.60 | kB |
| fds per open tenant, each read once | 5.00 | fds |
| cold open + first query, cache has room p50 | 0.38 | ms |
| cold open + first query, cache has room p99 | 17.21 | ms |
| cold open + first query, evicting a read-only tenant p50 | 0.34 | ms |
| cold open + first query, evicting a read-only tenant p99 | 0.54 | ms |
| RssAnon, all open, each written once | 29500 | kB |
| RssAnon per open tenant, each written once | 268.68 | kB |
| fds per open tenant, each written once | 5.00 | fds |
| warm query p50 | 0.01 | ms |
| warm query p99 | 0.02 | ms |
| cold open + first query, evicting a written tenant p50 | 10.20 | ms |
| cold open + first query, evicting a written tenant p99 | 25.98 | ms |
| RssAnon, after Close | 11640 | kB |

## 10-open-1000

| metric | value | unit |
|---|---:|---|
| tenants open at once | 1000 | tenants |
| RssAnon, nothing open | 2624 | kB |
| RssAnon, all open, each read once | 231220 | kB |
| RssAnon per open tenant, each read once | 228.60 | kB |
| fds per open tenant, each read once | 5.00 | fds |
| cold open + first query, cache has room p50 | 0.43 | ms |
| cold open + first query, cache has room p99 | 0.73 | ms |
| RssAnon, all open, each written once | 245456 | kB |
| RssAnon per open tenant, each written once | 242.83 | kB |
| fds per open tenant, each written once | 5.00 | fds |
| warm query p50 | 0.02 | ms |
| warm query p99 | 0.04 | ms |
| RssAnon, after Close | 32308 | kB |

## 10-random-uniform

| metric | value | unit |
|---|---:|---|
| tenants / MaxOpen / workers | 1000 / 64 / 16 |  |
| duration | 60.04 | s |
| requests (1 read tx + 1 write tx) | 21467 | requests |
| throughput | 357.57 | requests/s |
| cache hit rate | 6.25 | % |
| evictions | 20062 |  |
| request p50 | 47.99 | ms |
| request p99 | 55.04 | ms |
| request max | 90.21 | ms |
| peak open | 64 | tenants |
| failed requests | 0 |  |

## 10-random-zipf-1.1

| metric | value | unit |
|---|---:|---|
| tenants / MaxOpen / workers | 1000 / 64 / 16 |  |
| duration | 60.06 | s |
| requests (1 read tx + 1 write tx) | 39353 | requests |
| throughput | 655.25 | requests/s |
| cache hit rate | 59.08 | % |
| evictions | 16040 |  |
| request p50 | 0.25 | ms |
| request p99 | 72.27 | ms |
| request max | 202.66 | ms |
| peak open | 64 | tenants |
| failed requests | 0 |  |
