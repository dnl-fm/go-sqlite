# go-sqlite Consumer Upgrade Inventory

This is the local app inventory for moving consumers to the `go-sqlite` release
that contains the Turso 0.6.0 bump and `lab/turso-v060` evidence.

The target release is not published yet. Current public tags stop at `v0.5.0`,
and `origin/main` only has the earlier rowid-policy commit. Do not update apps
to `@main` for this work; it will not include the Turso 0.6.0 dependency bump.

## Consumers

| path | current |
|---|---|
| `/home/fightbulc/Buildspace/ato/mono/packages/subs` | `v0.2.0` |
| `/home/fightbulc/Buildspace/ato/payment-landing` | `v0.3.0` |
| `/home/fightbulc/Buildspace/ato/subs` | `v0.2.0` |
| `/home/fightbulc/Buildspace/dnl/adsense/headlines` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/adsense/hormuz-crisis/final` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/agent-mono/packages/email-go` | local replace to `../../../go-sqlite` |
| `/home/fightbulc/Buildspace/dnl/agent-mono/packages/n26-go` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/agent-mono/packages/ops-team/incidents-go` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/agent-mono/packages/ops-team/ops-node-go` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/agent-mono/packages/ops-team/sentinel-go` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/anycheck` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/callstats` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/claw/packages/clawd` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/gen-agent` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/granny/app` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/granny/forge` | `v0.3.0` |
| `/home/fightbulc/Buildspace/dnl/quitt` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/rawcall` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/run-layer/apps/api` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/run-layer/apps/backoffice` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/run-layer/tools/garmin` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/schaltwerk` | `v0.4.0` |
| `/home/fightbulc/Buildspace/dnl/vert` | `v0.5.0` |
| `/home/fightbulc/Buildspace/dnl/zeitnehmer/packages/api` | `v0.5.0` |

## Upgrade Order

1. Publish the new `go-sqlite` release.
2. Update direct consumers to that tag with `GOWORK=off go get github.com/dnl-fm/go-sqlite@<tag>`.
3. Run each app's documented gate.
4. For Turso MVCC apps, search migrations and schema builders for
   `WITHOUT ROWID` before release.
5. For apps needing live DB inspection, test the exact `tursodb 0.6.0`
   `--experimental-multiprocess-wal` workflow before documenting it as an
   operator path.

`email-go` already uses a local replace. It will see local `go-sqlite` changes
while developing inside this checkout, but that is not a release story.
