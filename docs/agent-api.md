# Consumer Agent Access

`go-sqlite` is a library, not a service. It exposes no HTTP endpoints or admin
socket. A consuming service that owns a live MVCC database must provide the
agent-facing reads its operators need.

## Access Contract

Agents and CLI tools call the owning app. They do not open or write canonical
database files or search projections.

Prefer named reads such as:

```text
appctl jobs list --status failed --limit 50
appctl customers get customer_01j...
appctl database health
```

These names illustrate the consumer pattern; they are not `go-sqlite` APIs.
The consuming service owns authentication, authorization, response schemas,
pagination, timestamps, and error behavior.

Safe read surfaces usually include health, detail, list/search, timelines,
counts, and bounded exports. Document which ones are safe in the consumer repo.
Writes require explicit user intent and must go through business operations,
never generic database access. Destructive operations require explicit
confirmation at the consuming service boundary.

## Generic Inspection

Named operations are the default. If generic query access is unavoidable, it
must provide all of these controls:

- admin authentication and authorization;
- read-only enforcement below the request parser;
- a hard query timeout;
- a hard row limit and bounded pagination;
- an audit record containing actor, query intent, time, and outcome.

Do not expose unrestricted SQL over HTTP or a socket.

## Search

The app-facing API owns search. `turso-searchd` owns a disposable projection
and returns IDs, facets, and match hints to the app. The app hydrates canonical
rows, applies permissions, and returns the public response. Agents do not call
the projection as if it were canonical and never write its database.

## Incident Inspection

For live state, use the owning app's authenticated reads. For offline
forensics, request an owner-produced snapshot or close the app cleanly, then
use `tursodb` against that artifact. Do not use system `sqlite3`, and do not
raw-copy an open Turso database.

See [Turso operations](turso-operations.md) for the storage-level boundary.
