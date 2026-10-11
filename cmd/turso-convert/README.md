# turso-convert

Turso MVCC files cannot open in standard SQLite. This command converts a stopped database into a separate SQLite WAL file.
The source and its sidecars stay byte-identical. The main go-sqlite module does not depend on this module or Turso.

Build from this directory:

```sh
CGO_ENABLED=0 GOWORK=off go build -o turso-convert .
./turso-convert --check /path/to/source.db
./turso-convert /path/to/source.db /path/to/converted.db
```

The output reports the format, table count, row count, source and output sizes, and duration.
An existing destination or destination sidecar causes an error. Conversion in place is refused.
Plain SQLite sources are verified and copied. Their report says `already SQLite`.

## Stop, convert, swap, start

1. Stop the service that owns the source database. Prevent automatic restarts and close other database clients.
2. Run `--check` to identify the source format.
3. Convert the source to a new file beside it.
4. Archive the original file and every original `-log`, `-wal`, `-shm`, and `-journal` sidecar together.
5. Install the converted file at the canonical path. Make sure that no original sidecar remains at that path.
6. Start the new SQLite version of the service.

Retain the complete original set for rollback to the old service version. Restore the entire set together while the service is stopped.
The command performs conversion only. It does not stop services, swap their canonical files, or remove the original files.

## What it verifies

The command copies the source and its sidecars into a temporary directory beside the destination.
For Turso sources, it opens only that copy with the pinned Turso 0.7.2 driver.
It verifies logical-log headers and all frame checksums before recovery, because Turso can otherwise discard a torn log tail.
After a checkpoint, the logical log must be empty or contain only its valid 56-byte header. The Turso WAL must be empty.

For Turso sources, the command records the user schema and each table's row count through Turso, then closes that engine.
It changes MVCC header bytes `ff ff` to `02 02` and archives staged Turso sidecars within the temporary directory.
SQLite removes the known Turso metadata and AUTOINCREMENT sequence tables.
The command transfers sequence high watermarks into `sqlite_sequence`, including IDs whose rows were deleted.
Unknown Turso internal objects cause an error rather than silent removal.

The output must pass SQLite `integrity_check`, preserve the schema and table counts, and preserve AUTOINCREMENT high watermarks.
Schema comparison excludes Turso internals and the engine-specific `sqlite_sequence` schema entry.
The output enters WAL mode and completes a truncate checkpoint before it closes.
Only the complete database file is installed. No staged sidecar is installed.

SHA-256 hashes, file identities, sizes, and modification times detect changes to the original file or sidecars during conversion.
Every copied file's hash must match its source snapshot. The final source snapshot must match the initial one.
Failures remove temporary output. Linux `renameat2(RENAME_NOREPLACE)` installs the result atomically without overwriting another file.
The command also syncs the output file and destination directory.

## Detection limits

Linux `F_GETLK` detects conflicting advisory record locks through read-only descriptors.
A temporary shared `flock` detects an existing exclusive flock without changing file bytes.
The command checks the database and all copied sidecars, including SQLite's shared-memory file.
Drivers that use other locking schemes remain outside these checks. Record locks held by this process itself are invisible to `F_GETLK`.

These checks detect activity. They cannot prove that the owning service is stopped.
An idle owner, transient changes between observations, or a process that starts after the final observation can escape detection.
The destination directory must remain under the operator's control: sidecar creation cannot be atomic with the database rename.
Even if no lock is detected, stop the owning service before conversion.

`--check` opens a disposable copy and never opens the source through a database engine.
An `ff ff` header identifies Turso MVCC. A compatible SQLite header with known Turso internal objects identifies Turso WAL.
A compatible WAL file without Turso markers is indistinguishable from plain SQLite and reports `plain SQLite`.
This classification describes observable format compatibility, not which program originally wrote the file.

The converter runs on Linux. It requires filesystem support for atomic `RENAME_NOREPLACE` and refuses an unsupported rename.
Source symlinks and non-regular files are refused. Destination directories containing `?` are refused because Turso treats it as a DSN delimiter.
The Go build needs no C compiler. The pinned Turso driver loads its platform library at runtime.
A missing native library produces an actionable error.

## Verification

```sh
GOWORK=off go vet ./...
GOWORK=off go test -count=1 ./...
GOWORK=off go test -race -count=1 ./...
gofmt -l .
CGO_ENABLED=0 GOWORK=off go build ./...
```

Tests create MVCC, Turso WAL, and plain SQLite fixtures, including a pending logical log and more than 10,000 rows.
They compare source and output schemas, table counts, and content checksums.
They also cover sequence continuation, corrupt logs, lock detection, source mutation, destination refusal, and atomic installation.
