# ADR Style Guide

This guide captures the conventions used across the NATS ADR collection. It exists alongside [adr-template.md](adr-template.md): the template gives you the skeleton, this guide explains how to fill it in.

ADR-50 (JetStream Batch Publishing) is a good reference for the full style — header tables, revision history, layered design sections, error tables, and embedded Go types.

> [!NOTE]  
> Claude extracted This guide from recent ADRs to capture the common style, when using Claude to help you write ADRs tell it to follow this guide.

## Purpose and voice

ADRs in this repository are **design specifications**, not RFCs.

- Write to engage implementers. The reader should understand the *why* behind the design and have room to make sensible local decisions.
- Avoid heavy use of RFC 2119 keywords (`MUST`, `SHOULD`, `MAY`). Plain prose is preferred for design discussion. Use `MUST`/`MUST NOT` only where conformance genuinely matters — for example, a server rejecting an unknown operation, or a client being required to subscribe before publishing.
- Where the document touches **wire-level protocol** — subject names, header names, JSON field names, error codes, advisory event types, configuration field names — adherence is strict. Spell these exactly as implementations will use them, and use backticks every time they appear in prose.
- Tone is collaborative and explanatory. It is fine, and often helpful, to write a paragraph explaining the motivation for a design choice ("It's a conscious decision to not use the `Error` field in the `PubAck` for this purpose…").
- Write in American English: prefer `behavior`, `serialize`, `synchronize`, `analyze`, `defense` over `behaviour`, `serialise`, `synchronise`, `analyse`, `defence`. Wire-level identifiers (header names, JSON fields, error codes) are spelled exactly as the implementation uses them, regardless of which dialect that produces.

## Filename and numbering

- Filename is `adr/ADR-<n>.md` where `<n>` is the next free integer. Numbers are never reused.
- After adding or editing an ADR, run `go run main.go > README.md` to regenerate the index. This also validates the metadata header.

## Title

The H1 is the human-readable feature title — e.g. `# JetStream Batch Publishing`, `# JetStream Message Scheduler`. No "ADR-N" prefix; that is implied by the filename and the index.

## Metadata header

The first table after the title is required and must match this shape:

```markdown
| Metadata | Value                                 |
|----------|---------------------------------------|
| Date     | YYYY-MM-DD                            |
| Author   | @githubuser, @githubuser              |
| Status   | Approved                              |
| Tags     | jetstream, server, client, 2.12, 2.14 |
```

- **Date** — ISO date (`YYYY-MM-DD`) of the original document.
- **Author** — one or more GitHub `@handles`, comma-separated.
- **Status** — one of `Proposed`, `Approved`, `Partially Implemented`, `Implemented`, `Deprecated`.
- **Tags** — comma-separated. Tags drive the README index, so reuse existing ones where possible:
  - Domain: `jetstream`, `kv`, `objectstore`, `security`, `observability`, `orbit`, `spec`
  - Audience: `server`, `client`
  - Server version(s) the work targets, e.g. `2.12`, `2.14` — include every version that contributed material to the document.
  - Lifecycle: `refinement`, `deprecated` when applicable.
- **Updates** — optional, add only when this ADR refines or supersedes another, e.g. `Updates | ADR-59`.

## Revision history

A new ADR does **not** need a revision history table — the metadata `Date` already records its origin. Add the table the first time the document is revised.

Once present, the table looks like this:

```markdown
| Revision | Date       | Author                      | Info                          | Server Version | API Level |
|----------|------------|-----------------------------|-------------------------------|----------------|-----------|
| 1        | 2025-06-10 | @ripienaar                  | Initial design                | 2.12.0         | 2         |
| 2        | 2025-09-08 | @MauriceVanVeen             | Initial release               | 2.12.0         | 2         |
```

- Revision `1` is the initial design, with the same date as the metadata header.
- Each subsequent revision gets a new row — never edit history in place.
- `Author` may list multiple `@handles` for jointly-authored revisions.
- `Info` is a short phrase describing the change ("Add server codes", "Support deduplication", "Clarify X behavior").
- `Server Version` and `API Level` columns are encouraged for ADRs tied to a release. Only include them if you can fill them in meaningfully; for non-server ADRs they can be omitted.

## Refinement ADRs

Some parent ADRs — KV (ADR-8), Object Store (ADR-20), JetStream Sourcing/Mirroring (ADR-59) — are large, complex, living specifications. When a new feature extends one of these, splicing the change directly into the parent makes it very hard for reviewers to see *what is actually changing* against everything else in the document. A **refinement ADR** is the answer: a separate document that captures only the delta needed to implement the new feature.

ADR-57 (KV Subject Transforms, refining ADR-8) is the model. Other examples: ADR-48, ADR-54, ADR-58, ADR-60.

How refinements relate to their parent:

- The refinement is **change-focused**. It describes only what is new or different — new APIs, new headers, new client behavior, new configuration. It does not restate the parent.
- The parent ADR remains the **full living specification**. Once the refinement is approved, its contents are typically folded back into the parent so that the parent continues to read as a complete description of the feature. The refinement document stays in place as the historical record of that change.
- This split exists for reviewability, not permanence: the refinement makes the diff legible; the merged parent makes the spec coherent for future readers.

When to write a refinement vs. a new revision of the parent:

- Use a **revision** (a new row in the parent's revision history) for small, localised changes that a reviewer can follow inline.
- Use a **refinement ADR** when the change is large enough that reading it inline against the rest of the parent would obscure what's new — typically a new sub-feature, a new API surface, or a new behavioral mode.

Conventions for refinement ADRs:

- **Metadata** — set the `Updates` field to the parent, e.g. `Updates | ADR-8`. The README index uses this to render the `(updating [ADR-8](adr/ADR-8.md))` suffix automatically; do not write that suffix into the title yourself.
- **Tags** — include `refinement` when the document is purely a refinement (see ADR-48). It is a soft convention rather than a hard rule — ADR-57 omits the tag while still being a refinement — but adding it makes the index easier to scan.
- **Opening paragraph** — state the relationship to the parent in the first sentence of the *Context* section. ADR-57 opens with: *"This ADR is a refinement to [ADR-8](adr/ADR-8.md), documenting how Key-Value sourcing and mirroring works in client libraries…"* Follow that pattern.
- **Scope** — describe only the new or changed material. Link to the relevant section of the parent for context rather than restating it.
- **Folding back** — once the refinement is approved and implemented, fold its substantive content into the parent ADR (under a new revision) so the parent stays a full specification. The refinement document is not deleted; it remains the readable record of the change.
- **API Level and limits** — apply the same rules as for any other ADR. A refinement that changes the wire surface bumps the API Level; a refinement that introduces new state needs the same bounded-resource treatment described under *Design principles*.

Refinement ADRs use the same metadata header, revision history rules, and body structure as any other ADR — the *Body structure* section below applies unchanged.

## Body structure

Body headings (H2 and below) use **sentence case**: capitalize only the first word and any proper nouns. Write `Server behavior design`, not `Server Behavior Design`. Acronyms (`API`, `ADR`, `KV`, `JSON`) and wire-level identifiers (`StreamConfig`, `Nats-Batch-Id`) keep their canonical form. The H1 title follows the *Title* rule above and may be a product or feature name in its natural form.

The headings inside the body are ADR-specific — pick what fits the design. The template's section names are starting points, not a required schema. ADRs commonly mix and match from this set:

- `Context` / `Context and problem statement` / `Context and motivation` / `Background`
- `Goals` / `Non-goals`
- `Design` / `Solution overview` / `Architecture`
- Per-component sections: `Client design`, `Server design`, `Server behavior design`, `Stream configuration`
- Specifics: `Headers`, `Subjects`, `Server errors`, `Advisories`, `Publish acknowledgements`
- `Decision`, `Consequences`, `Alternatives considered`, `Future work`

Whatever names you choose, the document should answer these questions in roughly this order:

1. **Why does this exist?** A *Problem statement* or *Context* section that explains the user-visible problem, with a concrete scenario where useful (ADR-50's "address split across 5 KV keys" is a good example). Cover prior art or related work if it informs the design.
2. **What is the shape of the solution?** A *Design* section that gives the conceptual model before any wire details — the moving pieces, what the client does, what the server does, how they cooperate.
3. **Specifics.** The protocol-level details an implementer needs to be exact about:
   - Subjects and subject patterns (use backticks; show the full template, e.g. `<prefix>.<uuid>.<flow>.<gap>.<seq>.<op>.$FI`)
   - Header names and accepted values (e.g. `Nats-Batch-Id`, `Nats-Schedule-Target`)
   - JSON message types — embed Go structs with their `json:` tags, plus a one-line example payload
   - Error codes in a table: `ErrCode | Code | Description`
   - Limits and timeouts as bullet lists ("each stream may have 50 batches in flight", "abandoned after 10s of silence")
   - Configuration changes — `StreamConfig` field additions, API level bumps
   - Advisory event types and their JSON shape
4. **Edge cases and interactions.** Behavior under leader change, mirrors and sources, interaction with existing features (TTL, retention, dedupe), version compatibility.

For larger documents (ADR-50 covers two related sub-features), introduce a top-level section per sub-feature, then repeat the Context → Design → Specifics layering inside each.

## Conventions for document layout and style

### General

These details show up in nearly every ADR — be consistent:

- **Subject patterns** — backtick the whole pattern, use angle-bracketed placeholders: `<prefix>.<uuid>.<seq>.$FI`. Document each placeholder.
- **Headers** — backtick every header reference (`Nats-Batch-Id`). When introducing a header, give a concrete example value (`Nats-Batch-Sequence:1`).
- **Go types** — use a fenced ```go block. Keep struct comments brief; they will be read as the spec. Include `json:` tags exactly as the wire format requires. Show a one-line example JSON payload immediately after the struct.
- **Error tables** — three columns, `ErrCode | Code | Description`. `ErrCode` is the NATS-internal numeric code, `Code` is the HTTP-style status. Keep the description short and parenthesise the offending header or value.
- **Limits** — express them as bullet lists with concrete numbers. State per-stream and per-server limits separately.
- **Shell examples** — fenced ```bash blocks using the `nats` CLI to demonstrate user-visible behavior. Useful in the early sections to ground the design in something tangible.
- **Field names** — when referencing struct fields in prose, backtick them (`AllowAtomicPublish`, `BatchSize`).

### Cross-references

- Link to other ADRs as `[ADR-8](adr/ADR-8.md)` from the README, or `ADR-8` in inline prose. The index already links them, so plain text is fine in body copy.
- When refining another ADR, set the `Updates` metadata field and call out the relationship in the opening paragraph.
- External references (blog posts, papers) go under a short `Related:` bullet list in the context section.

### What to avoid

- Don't fill the document with `MUST` / `SHOULD` / `MAY` language as if it were an IETF RFC.
- Don't document individual minor decisions as standalone ADRs — see the README's "When to write an ADR" section.
- Don't edit prior revisions in place to reflect a change of mind. Add a new revision row and update the body.
- Don't invent variations of established protocol names (header casing, JSON field names) — match the existing convention exactly.
- 
## Design principles

When designing server features, we follow the principals below, now all ADRs require these sections or require this to be checked against. If in doubt ask the user if this is a NATS Server feature that might need these checks and balanced.

### No unbound resource usage

ADRs describe behavior that runs inside long-lived servers. Designs must make that sustainable. The single most important principle: **nothing in the design may grow without a bound.** Anything that consumes memory, holds state, queues work, or extends startup or runtime cost has to have an explicit cap.

When proposing a feature, identify every place it touches resource consumption and state the limit in the document. Typical categories:

- **In-flight state per stream** — pending operations, open sessions, partially constructed batches. Cap it.
- **In-flight state per server** — the aggregate across all streams. Cap it separately; per-stream caps alone are not enough on a busy server.
- **Per-operation size** — number of messages in a batch, number of headers, length of an identifier (ADR-50 caps batch IDs at 64 characters and atomic batches at 1000 messages).
- **Idle / abandonment timeouts** — anything held on behalf of a client must be released if the client goes away. ADR-50 abandons batches with no traffic for 10 seconds and emits an advisory.
- **Queue depth and buffer growth** — never an unbounded channel or slice. If the producer can outrun the consumer, define what happens at the limit (drop, reject, flow-control, advisory).
- **Startup cost** — anything replayed or rebuilt at boot must be bounded, or have a strategy (compaction, snapshotting, TTL) that keeps it bounded.
- **Persisted state per stream** — if the feature stores something on the stream itself (schedules, counters, tracked IDs), say what bounds the count.

Every limit needs a concrete default value in the ADR, not just "configurable". ADR-50 is the model here:

> - Each stream can only have 50 batches in flight at any time
> - Each server can only have 1,000 batches in flight at any time
> - A batch that has not had traffic for 10 seconds since the last message will be abandoned
> - Each batch can have maximum 1000 messages

State the limits as a bullet list under a *Server behavior design* (or equivalent) heading, with the per-stream and per-server caps separated. Where the limit is enforced by sending an error, cross-reference the error code in the *Server errors* table. Where it is enforced silently (idle abandonment), say what advisory is raised so operators can observe it.

When the design genuinely needs unbounded behavior — for example ADR-50's fast-ingest batches have no message-count cap — call that out explicitly and explain what other mechanism (in that case, server-driven flow control and per-server batch caps) keeps the system safe. "Unbounded" is never the default; it is a deliberate exception that must be justified.

### Super-cluster topology

Any feature anchored to a single Stream, KV, Object Store, or Consumer is, by definition, anchored to one cluster within a super-cluster — the cluster that holds the asset. That cluster is a single point of failure for the feature, and the asset cannot simply be stretched across the super-cluster to fix it. ADRs must acknowledge this and describe how the feature behaves and how users are expected to operate it in that topology.

At minimum, cover:

- **Locality.** State explicitly that the feature is scoped to the cluster holding the underlying Stream/KV/etc., and what clients in other clusters see (transparent proxying, additional latency, errors, nothing at all).
- **Failure mode.** What happens when the holding cluster is unreachable from the rest of the super-cluster — does the feature degrade, fail loudly, or silently stall? How is that surfaced to clients?
- **Mitigation strategies.** Describe at least one supported pattern for users who need higher availability than a single cluster provides. Typical options:
  - Mirror or source the underlying asset into other clusters and define what the mirrored copy can and cannot do (read-only? eventually consistent? feature-disabled?).
  - Run independent instances of the feature per cluster and document how identifiers, subjects, and configuration must be partitioned to avoid collisions.
  - Explicitly state that the feature is not appropriate for cross-cluster use and recommend an alternative.
- **Interaction with mirrors and sources.** Spell out which headers, configuration flags, or behaviors are honored, ignored, or rejected on a mirrored or sourced copy. ADR-50's *Mirrors and Sources* section is the model — short, concrete, and decisive.

A feature without a documented super-cluster story is incomplete. If the answer is "this only works inside one cluster, full stop," that is a valid answer — but it has to be in the ADR.

### API Level

JetStream uses an API Level to let clients negotiate which server features they can rely on. Any change to the wire-visible API surface must bump the API Level, and the ADR must say so.

Bump the API Level whenever the design:

- Adds a new field to an existing struct that crosses the wire (`StreamConfig`, `ConsumerConfig`, `PubAck`, advisory payloads, etc.).
- Changes the meaning, type, or required-ness of an existing field.
- Adds a new struct, message type, advisory, or API endpoint that clients must be able to recognize.
- Adds new accepted values to an enum-like field (operation codes, status strings, header values) where a client that doesn't know the value would behave incorrectly.

Renames and field removals are stronger than a level bump and should be avoided in favor of additive change; if unavoidable, the ADR must spell out the migration.

Every ADR that changes the API surface must call out the **required API Level** for the feature in two places:

- The revision history table, in the `API Level` column, against the revision that introduced or changed the API. ADR-50 is the model — its rows carry `API Level` `2` and `4` next to the corresponding `Server Version`.
- In prose, where the feature's enabling configuration is described. ADR-50 does this directly: *"Setting `AllowAtomicPublish` to true should set the API level to 2, setting `AllowBatchPublish` to true should set the API level to 3."*

If a single ADR contains multiple sub-features that ship at different times, give each sub-feature its own API Level statement — don't fold them together. Clients use the level to gate behavior, so the document must make it unambiguous which level unlocks which capability.

ADR-44 (Versioning for JetStream Assets) is the authoritative reference for how API Level works; cross-reference it rather than restating the mechanism.

