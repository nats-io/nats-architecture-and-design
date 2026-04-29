# Conformance Rules

You are helping the user interpret a ADR file and provide a 3rd-party automated consensus by turning the ADR file into a set of rules.

# Extracting rules from a ADR file

- Understand the example conformance rules file in `conformance/ADR-50-fast-batch.md` as a template
- Read the adr file stored in `adr/ADR-xxx.md`
- Ask questions if something is not clearly understood
- If there are sections not in scope for conformance testing ask the user for confirmation
- Write the conformance rules to a Markdown file in `conformance/ADR-xxx.md`. If the user is asking for a subset of tests
  save them in a file like `conformance/ADR-xxx-subset.md`.
- Present the user a summary of what was done, what is out of scope or specifically skipped
- Clearly report ambiguities or errors in the ADR file

# Example Rule

Rules are grouped into sections:

```
## AB-100 — Stream configuration
```

Each section holds many rules:

```
### AB-101 — Enabling `AllowAtomicPublish` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowAtomicPublish: true`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowAtomicPublish` is reported as `true`.
  - The stream's reported API level is at least `2`.
```