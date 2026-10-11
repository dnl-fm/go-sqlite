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
commands. `local-replace` rows use a checkout instead of their version pin.
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
5. For v0.8.0, apply the [API and storage upgrade](consumer-guide.md#upgrading-from-v07x).
6. Stop the owning service and convert old-format files before the new version starts.
7. Run authenticated health and read commands through the owning app.
8. Verify snapshots with `sqlite3 -readonly`, schema comparison, and relevant row counts.
