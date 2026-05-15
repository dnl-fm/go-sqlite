# Turso 0.6.0 Consumer Guide

Turso 0.6.0 added real engine behavior, but consumers should not flatten it
into "Turso supports everything now." The details matter.

## What changed

`go-sqlite` now depends on `turso.tech/database/tursogo v0.6.0`. The Go API did
not materially change, but the embedded engine did.

| Feature | Consumer rule |
|---|---|
| `WITHOUT ROWID` | Plain Turso can use it only with `?experimental=without_rowid`. Turso MVCC still rejects writes to those tables. |
| MVCC writes | Keep using `WithTursoMVCC()` plus `ConcurrentTxRetry` / `BEGIN CONCURRENT` on one reserved connection. |
| Live CLI inspection | Use `db?experimental=multiprocess_wal` in the Go app and `tursodb --experimental-multiprocess-wal db ...` in the CLI. |
| Concurrent Go writer processes | Not a promise. The lab still sees WAL file-locking with multiple Go child writers through `tursogo v0.6.0`. |

## Upgrade checklist for apps

1. Bump `github.com/dnl-fm/go-sqlite` to the release that contains this guide.
2. Run the app's normal Go test gate with `GOWORK=off` unless the repo documents
   a workspace-specific flow.
3. Search migrations for `WITHOUT ROWID`.
4. If the app uses `WithTursoMVCC()`, keep `WITHOUT ROWID` out of its schema.
5. If the app needs live production inspection, test the exact operator path
   with `tursodb 0.6.0` and `--experimental-multiprocess-wal`.

The lab evidence lives in `lab/turso-v060`. It is intentionally not part of
`pkg/` because it tests upstream behavior we may or may not wrap.
