# github.com/dnl-fm/go-sqlite

## Stack

- go
- sqlite

## Components

| Name | Path | Type |
|------|------|------|
| migrate | `cmd/migrate/` | cli |
| pkg | `pkg/` | lib |

## Commands

```bash
# Test
make test

# Build migrate CLI
make build
```

## Specs

**IMPORTANT:** Before implementing or debugging, consult the specifications in `specs/README.md`.

### Principles

- **Specs are intent, code is reality.** Specs describe what SHOULD exist; always verify against actual code.
- **Don't assume not implemented.** Search the codebase first before concluding something needs to be built.
- **Use specs as guidance.** Follow the design patterns, types, and architecture defined in relevant specs.
- **Cite sections in plans.** Use `§` references (e.g., `managers.md §4.2`) for traceability.

### Spec Format

Every spec file uses frontmatter for metadata:

```yaml
---
tags: [auth, oauth, sessions]      # Searchable tags
status: implemented                # planned | implemented | deprecated
owner: internal/auth/              # Primary code location
integrations: [firebase-auth]      # External systems
---
```

### Workflow

```bash
ralph reindex               # Extract specs from existing code
ralph feature "description" # Create feature spec + plan
ralph debug "description"   # Create debug spec + plan
ralph plan <spec>           # Generate implementation plan
ralph <plan>                # Execute plan
ralph verify <spec>         # Verify implementation matches spec
```
