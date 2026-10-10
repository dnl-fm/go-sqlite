# go-sqlite Consumer Upgrade Inventory

Static consumer tables rot the moment another `go.mod` lands. Generate the
inventory from `~/werk` instead:

```bash
make consumer-build
./bin/go-sqlite-consumers scan ~/werk
```

The scanner queries the latest published tag by default. Pin the comparison
when planning an unreleased version:

```bash
./bin/go-sqlite-consumers scan ~/werk --target v0.8.0
```

It is read-only. `outdated` rows include exact `go get` and `go mod tidy`
commands. `local-replace` rows use a checkout instead of their version pin;
`legacy-replace` rows need a module-path migration rather than a blind bump.

For automation, use stable JSON or make stale published pins fail the command:

```bash
./bin/go-sqlite-consumers scan ~/werk --json
./bin/go-sqlite-consumers scan ~/werk --check
```

## Upgrade Order

1. Publish the new `go-sqlite` release.
2. Scan with `--target <tag>` and review local/legacy replacements separately.
3. Run each printed upgrade command one repository at a time.
4. Run each app's documented gate.
5. For Turso MVCC apps, search migrations and schema builders for
   `WITHOUT ROWID` before release.
6. For apps needing live DB inspection, expose named authenticated reads or a
   guarded local admin socket through the owning service. Do not open its live
   MVCC file from a second process.
7. For offline forensics, verify an owner-produced snapshot or cleanly closed
   DB with `tursodb`, not system `sqlite3`.
