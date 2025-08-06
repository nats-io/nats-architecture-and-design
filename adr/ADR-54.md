# KV Codecs

| Metadata | Value                              |
|----------|------------------------------------|
| Date     | 2025-08-06                         |
| Author   | @piotrpio                          |
| Status   | Proposed                           |
| Tags     | jetstream, client, spec, orbit, kv |
| Updates  | ADR-8                              |

| Revision | Date       | Author    | Info           |
|----------|------------|-----------|----------------|
| 1        | 2025-08-06 | @piotrpio | Initial design |

## Context and Problem Statement

JetStream Key-Value stores require flexible data transformation capabilities to handle various encoding scenarios such as character escaping, path notation conversion, encryption, and custom transformations. Currently, these transformations must be implemented at the application level, leading to inconsistent implementations and increased complexity.

A standardized codec system would provide transparent encoding and decoding of keys and values while maintaining full compatibility with the existing KV API, enabling users to handle special characters, implement security features, and perform custom transformations seamlessly.

## Context

The JetStream Key-Value store uses NATS subjects as keys and message payloads as values. Several use cases require transformation of these elements:

1. **Character Escaping**: Keys containing special characters (spaces, dots, etc.) that are invalid in NATS subjects
2. **Path Translation**: Converting between different path notation styles (e.g., filesystem paths to NATS subjects)
3. **Security**: End-to-end encryption
4. **Custom Transformations**: Application-specific encoding requirements

## Design

### Core Interfaces

The codec system is based on two separate interfaces for transforming keys and values:

```go
// KeyCodec transforms keys before storage and after retrieval
type KeyCodec interface {
    Encode(key string) string
    Decode(encoded string) string
}

// ValueCodec transforms values before storage and after retrieval
type ValueCodec interface {
    Encode(value []byte) []byte
    Decode(encoded []byte) []byte
}
```

### Creating a Codec-Enabled KV Bucket

A codec-enabled KV bucket wraps the standard KV interface. It should be possible to use codecs for both keys and values independently, allowing for flexible transformations.

```go
type CodecKV struct {
    kv         jetstream.KeyValue
    keyCodec   KeyCodec
    valueCodec ValueCodec
}

// Constructor functions
func New(kv jetstream.KeyValue, keyCodec KeyCodec, valueCodec ValueCodec) jetstream.KeyValue
func NewForKey(kv jetstream.KeyValue, keyCodec KeyCodec) jetstream.KeyValue
func NewForValue(kv jetstream.KeyValue, valueCodec ValueCodec) jetstream.KeyValue
```

### Transparent Operation

All KV operations work transparently with codecs:

```go
// Standard KV operations
err := codecKV.Put(ctx, key, value)
entry, err := codecKV.Get(ctx, key)
watcher, err := codecKV.Watch(ctx, pattern)
keys, err := codecKV.Keys(ctx)

// History and other operations
history, err := codecKV.History(ctx, key)
err := codecKV.Delete(ctx, key)
err := codecKV.Purge(ctx, key)
```

### Custom Codec Implementation

Users can implement custom codecs for specific requirements:

```go
type AESCodec struct {
    cipher cipher.Block
}

func (c *AESCodec) Encode(value []byte) []byte {
    // Implement AES encryption
    return encrypted
}

func (c *AESCodec) Decode(value []byte) []byte {
    // Implement AES decryption
    return decrypted
}

// Usage
aesCodec := &AESCodec{cipher: aesCipher}
codecKV := kvcodec.NewForValue(kv, aesCodec)
```

### Watch and Wildcard Support

Codecs should support wildcard patterns for watching and listing keys. The `KeyCodec` interface can be extended to handle wildcard patterns, allowing users to specify how wildcards should be encoded and decoded.

Special handling of wildcards should be optional when implementing custom codecs, in a language idiomatic way.

```go
type FilterableKeyCodec interface {
    KeyCodec
    EncodeFilter(filter string) (string, error)
}

// Example implementation
func (c *CustomCodec) EncodeFilter(filter string) (string, error) {
    // Handle wildcard patterns specially to preserve filtering
    return encodePreservingWildcards(filter), nil
}
```

### Codec Chaining

It should be possible to chain multiple codecs together, allowing for complex transformations. Keys or values should be processed through each codec in sequence and decoded in reverse order.

```g
keyChain, _ := kvcodec.NewKeyChainCodec(pathCodec, base64Codec)
valueChain, _ := kvcodec.NewValueChainCodec(aesCodec, base64Codec)
codecKV := kvcodec.New(kv, keyChain, valueChain)
```

### Built-in Codecs

#### 1. NoOpCodec

Passes data through unchanged, useful for selective encoding:

```go
type NoOpCodec struct{}

func (c NoOpCodec) Encode(s string) string { return s }
func (c NoOpCodec) Decode(s string) string { return s }
```

#### 2. Base64Codec

Encodes keys/values using url-encoded base64:

```go
type Base64Codec struct{}

// Example usage:
codec := kvcodec.Base64Codec()
codecKV := kvcodec.New(kv, codec, kvcodec.NoOpCodec())

// "Acme Inc.contact" becomes "QWNtZSBJbmMuY29udGFjdA=="
codecKV.Put(ctx, "Acme Inc.contact", []byte("info@acme.com"))
```

#### 3. PathCodec

Translates between path-style and NATS-style keys:

```go
type PathCodec struct {
    separator string
}

// Example: converts "user/profile/settings" to "user.profile.settings"
codec := kvcodec.NewPathCodec("/")
codecKV := kvcodec.NewForKey(kv, codec)
```

As encoding leading and trailing slashes cannot be preserved directly as leading and trailing dots, the codec should handle these cases by encoding leading slashes as `_root_` and trimming trailing slashes.
