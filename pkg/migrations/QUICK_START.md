# Migration quick start

The runner uses registered Go functions. Import the app's migration package
before the runner starts.

1. Generate a migration:

   ```bash
   export DATABASE_URL="file:./app.db"
   migrate create add_users_table
   ```

2. Implement the generated `Up` and `Down` functions.
3. Compile the migration package into the app's runner.
4. Run pending migrations through the writer:

   ```go
   import (
       _ "example.com/app/migrations"
       "github.com/dnl-fm/go-sqlite/pkg/database"
       "github.com/dnl-fm/go-sqlite/pkg/migrations"
   )

   db, err := database.Open(ctx, "app.db")
   if err != nil {
       return err
   }
   defer db.Close()
   err = migrations.Run(ctx, db.Writer())
   ```

5. Exercise both migration directions with the app's test gate.

Versions run in order. Successful versions do not run again.
Each migration function owns any transaction it needs.
See [the migration README](README.md) for the API and CLI commands.
