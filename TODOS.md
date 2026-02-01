# TODOs

## Query Package

- [ ] Look into making queries compile-safe (type-safe query building at compile time)

  **Approaches:**

  1. **Code generation (sqlc-style)** - Generate Go code from SQL files at build time
     - Pros: Full type safety, catches SQL errors at build time
     - Cons: Requires build step, less flexible for dynamic queries
     - Reference: https://sqlc.dev

  2. **Generic builder pattern** - Use struct tags + generics to build queries
     ```go
     q := query.Select[User]().
         Where(query.Eq("email", "alice@test.com")).
         Build()
     ```
     - Pros: No code gen, works with existing structs
     - Cons: Runtime validation of field names (unless using reflection at init)

  3. **Struct field constants** - Manual but safe
     ```go
     var UserFields = struct { ID, Email string }{ ID: "id", Email: "email" }
     query.Build("SELECT * FROM users WHERE "+UserFields.Email+" = :email", ...)
     ```
     - Pros: Simple, typos caught at compile time
     - Cons: Manual maintenance

  **Recommendation:** sqlc is gold standard for true compile-time safety. Option 2 (generic builder) could complement existing `query.Build()` for type-safe cases.
