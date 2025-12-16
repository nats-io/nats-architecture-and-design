# KV Subject Transforms

| Metadata | Value                              |
|----------|------------------------------------|
| Date     | 2025-12-09                         |
| Author   | @piotrpio                          |
| Status   | Proposed                           |
| Tags     | jetstream, client, spec, kv        |
| Updates  | ADR-8                              |

| Revision | Date       | Author    | Info           |
|----------|------------|-----------|----------------|
| 1        | 2025-12-09 | @piotrpio | Initial design |

## Context and Problem Statement

This ADR is a refinement to [ADR-8](ADR-8.md), documenting how Key-Value sourcing and mirroring works in client libraries, including automatic subject transforms and support for custom mappings.

## Mirror Configuration

When a `Mirror` is configured in `KeyValueConfig`, clients automatically:

1. Prefix the mirror stream name with `KV_` if not already prefixed
2. Enable `MirrorDirect` on the underlying stream configuration

```go
if cfg.Mirror != nil {
    m := cfg.Mirror.copy()
    if !strings.HasPrefix(m.Name, kvBucketNamePre) {
        m.Name = fmt.Sprintf(kvBucketNameTmpl, m.Name)
    }
    scfg.Mirror = m
    scfg.MirrorDirect = true
}
```

This ensures that mirrored buckets:

- Follow the KV naming convention (`KV_<bucket>`)
- Support direct reads via the Direct GET API
- Automatically participate in RTT-based replica selection

## Source Configuration

### Automatic Subject Transforms

When creating a bucket, if `Sources` are configured without explicit subject transforms, clients assume the sources are KV buckets and automatically create subject transforms to map keys correctly:

```go
for _, ss := range cfg.Sources {
    // Ensure source stream name has KV_ prefix or add it
    if !strings.HasPrefix(ss.Name, kvBucketNamePre) {
        ss.Name = fmt.Sprintf(kvBucketNameTmpl, ss.Name)
    }

    // Automatically create subject transforms for KV-to-KV mapping
    ss.SubjectTransforms = []SubjectTransformConfig{
        {
            Source:      fmt.Sprintf("$KV.%s.>", sourceBucketName),
            Destination: fmt.Sprintf("$KV.%s.>", cfg.Bucket)
        }
    }

    scfg.Sources = append(scfg.Sources, ss)
}
```

For example, sourcing from bucket `ORDERS` into bucket `NEW_ORDERS` with a key filter `NEW.>` creates this transform:

```json
{
  "sources": [
    {
      "name": "KV_ORDERS",
      "subject_transforms": [
        {
          "src": "$KV.ORDERS.NEW.>",
          "dest": "$KV.NEW_ORDERS.>"
        }
      ]
    }
  ]
}
```

This automatic mapping ensures that:

- Keys from the source bucket appear correctly in the destination bucket
- No manual subject transform configuration is needed for KV-to-KV sourcing

### Custom Subject Transforms

To enable advanced use cases such as sourcing from non-KV streams or implementing custom key mappings, clients now support custom subject transforms.

### Behavior

When `SubjectTransforms` are explicitly provided in a `KeyValueConfig.Sources` configuration, clients should preserve them exactly as specified and skip automatic KV subject transform generation:

```go
for _, ss := range cfg.Sources {
    // If subject transforms are already set, use them as-is
    // This allows using non-KV streams as sources
    if len(ss.SubjectTransforms) > 0 {
        scfg.Sources = append(scfg.Sources, ss)
    } else {
        // Otherwise, apply automatic KV-to-KV transforms (as above)
    }
    // ...
}
```

### Use Case: Non-KV Stream as Source

This enables using regular JetStream streams as KV sources. For example, sourcing events from a stream `EVENTS` into a KV bucket `EVENT_CACHE`:

```go
kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket: "EVENT_CACHE",
    Sources: []*jetstream.StreamSource{
        {
            Name: "EVENTS",
            SubjectTransforms: []jetstream.SubjectTransformConfig{
                {
                    Source:      "events.processed.>",
                    Destination: "$KV.EVENT_CACHE.>",
                },
            },
        },
    },
})
```

This configuration:

- Sources messages from the `EVENTS` stream (not a KV bucket)
- Maps subjects `events.processed.*` to KV keys in `EVENT_CACHE`
- Allows using any stream as a KV data source

### Use Case: Custom Key Mapping

Custom transforms also enable advanced key mapping between KV buckets:

```go
kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket: "PRODUCTS",
    Sources: []*jetstream.StreamSource{
        {
            Name: "KV_INVENTORY",
            SubjectTransforms: []jetstream.SubjectTransformConfig{
                {
                    Source:      "$KV.INVENTORY.warehouse.*.product.>",
                    Destination: "$KV.PRODUCTS.>",
                },
            },
        },
    },
})
```

This maps keys like `warehouse.nyc.product.item123` from `INVENTORY` to `item123` in `PRODUCTS`.

## Implementation Requirements

Client libraries implementing KV sourcing must:

1. **For Mirrors:**
   - Automatically prefix mirror stream names with `KV_` if not present
   - Always enable `MirrorDirect` on the underlying stream configuration

2. **For Sources without custom transforms:**
   - Prefix source stream names with `KV_` if not present
   - Automatically generate subject transforms mapping `$KV.<source>.>` to `$KV.<bucket>.>`

3. **For Sources with custom transforms:**
   - Preserve the `SubjectTransforms` configuration exactly as provided
   - Skip automatic transform generation
   - Do not modify the source stream name prefix
