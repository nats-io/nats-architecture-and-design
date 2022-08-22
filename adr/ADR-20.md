# JetStream based Object Stores

|Metadata|Value|
|--------|-----|
|Date    |2021-11-03|
|Author  |@scottf|
|Status  |Partially Implemented|
|Tags    |jetstream, client, objectstore|

## Context

This document describes a design of a JetStream backed object store.

## Overview

We intend to hit a basic initial feature set as below, with some future facing goals as indicated:

Initial feature list:

- Represent an object store.
- Store a large quantity of related bytes in chunks as a single object.
- Retrieve all the bytes from a single object
- Store meta data regarding each object
- Store multiple objects in a single store  
- Ability to specify chunk size
- Ability to delete an object
- Ability to understand the state of the object store.

Possible future features

- Event Notifications (add/delete/lock)
- Locking
- Archiving (tiered storage)
- Searching/Indexing (tagging)
- Versioning / Revisions
- Overriding digest algorithm 
- Capturing Content-Type (mime type)
- Per chunk Content-Encoding (i.e. gzip)
- Read an individual chunk. 

## Basic Design

- Object store or bucket is backed by a stream
- Multiple objects can be placed in each bucket
- Object Info is stored as json in the payload of the message on the Object Info message subject.
- The Object Info subject is always rolled up (per subject)
- Object chunks are stored as the payload of messages on the Chunk message subject

## Naming Specification

Protocol Naming Conventions are fully defined in [ADR-6](ADR-6.md)

### Object Store
The object store name or bucket name (`bucket`) will be used to formulate a stream name 
and is specified as: `restricted-term` (1 or more of `A-Z, a-z, 0-9, dash, underscore`)

### Object Id
Object ids (`object-nuid`) is a nuid.

### Object Name
An individual object name is not restricted. It is base64 encoded to form `name-encoded`.

### Digest
Currently `SHA-256` is the only supported digest. Please use the uppercase form as in [RFC-6234](https://www.rfc-editor.org/rfc/rfc6234)
when specifying the digest as in `SHA-256=3F9239B0272558CE6E3E98731C43A654AC6882C5050D5F352B2A5BFBB8DE0058`.

### Default Settings

Default settings can be overridden on a per object basis.

| Setting | Value | Notes |
| --- | --- | --- |
| Chunk Size | 128k (128 * 1024) | Clients may tune this as appropriate. |

## ObjectStore / Stream Config

The object store config is the basis for the stream configuration and maps to fields
in the stream config like in KV.

```go
type ObjectStoreConfig struct {
	Bucket      string        // used in stream name template
	Description string        // stream description
	TTL         time.Duration // stream max_age
	MaxBytes    int64         // stream max_bytes
	Storage     StorageType   // stream storate_type
	Replicas    int           // stream replicas
	Placement   *Placement    // stream placement
}
```

### Stream Configuration and Subject Templates

| Component | Template |
| --- | --- |
| Stream Name | `OBJ_<bucket>` |
| Chunk Stream subject | `$O.<bucket>.C.>` |
| Meta Stream subject | `$O.<bucket>.M.>` |
| Chunk message subject | `$O.<bucket>.C.<object-nuid>` |
| Meta message subject | `$O.<bucket>.M.<name-encoded>` |


### Example Stream Config
```json
{
  "name": "OBJ_MY-STORE",
  "description" : "description",
  "subjects": [
    "$O.MY-STORE.C.>",
    "$O.MY-STORE.M.>"
  ],
  "max_age": 0,
  "max_bytes": -1,
  "storage": "file",
  "num_replicas": 1,
  "rollup_hdrs": true,
  "allow_direct": true,
  "discard": "new",
  "placement": {
    "cluster": "clstr",
    "tags": ["tag1", "tag2"]
  }
}
```

## Structures


### ObjectLink is used to embed links to other buckets and objects.

```go
type ObjectLink struct {
    // Bucket is the name of the other object store.
    Bucket string `json:"bucket"`
    
    // Name can be used to link to a single object.
    // If empty means this is a link to the whole store, like a directory.
    Name string `json:"name,omitempty"`
}
```

### ObjectMetaOptions

```go
type ObjectMetaOptions struct {
    Link      *ObjectLink `json:"link,omitempty"`
    ChunkSize uint32      `json:"max_chunk_size,omitempty"`
}
```

### ObjectMeta 

Object Meta is high level information about an object.

```go
type ObjectMeta struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Headers     Header `json:"headers,omitempty"`

    // Optional options.
    Opts *ObjectMetaOptions `json:"options,omitempty"`
}
```

### ObjectInfo 

Object Info is meta plus instance information. 
The fields in ObjectMeta are serialized in line as if they were 
direct fields of ObjectInfo 

```go
type ObjectInfo struct {
    ObjectMeta
    
    Bucket  string    `json:"bucket"`
    
    NUID    string    `json:"nuid"`
    
    // the total object size in bytes
    Size    uint64    `json:"size"`
    
    ModTime time.Time `json:"mtime"`
    
    // the total number of chunks
    Chunks  uint32    `json:"chunks"`
    
    // as in http, <digest-algorithm>=<digest-value>
    Digest  string    `json:"digest,omitempty"`
    
    Deleted bool      `json:"deleted,omitempty"`
}
```

### ObjectInfo Storage

The ObjectInfo is stored as json as the payload of the message under the Meta message subject.
The `ModTime` (`mtime`) is never written as part of what is being stored.

When the ObjectInfo message is retrieved from the server, use the message metadata timestamp as the
`ModTime`

#### Example ObjectInfo Json

```json
{
	"name": "object-name",
	"description": "object-desc",
	"headers": {
		"h1": "foo",
		"h2": "bar"
	}
	"options": {
		"link": {
			"bucket": "link-to-bucket",
			"name": "link-to-name"
		},
		"max_chunk_size": 1024
	},
	"bucket": "object-bucket",
	"nuid": "CkuyLEX4z2hbyjj1aWCfiH",
	"size": 9999,
	"chunks": 42,
	"digest": "SHA-256=abcdefghijklmnopqrstuvwxyz=",
	"deleted": true
}
```


### ObjectStoreStatus

The status of an object

```go
type ObjectStoreStatus interface {
    // Bucket is the name of the bucket
    Bucket() string
    
    // Description is the description supplied when creating the bucket
    Description() string
    
    // TTL indicates how long objects are kept in the bucket
    TTL() time.Duration
    
    // Storage indicates the underlying JetStream storage technology used to store data
    Storage() StorageType
    
    // Replicas indicates how many storage replicas are kept for the data in the bucket
    Replicas() int
    
    // Sealed indicates the stream is sealed and cannot be modified in any way
    Sealed() bool
    
    // Size is the combined size of all data in the bucket including metadata, in bytes
    Size() uint64
    
    // BackingStore provides details about the underlying storage. 
    // Currently the only supported value is `JetStream`
    BackingStore() string
}    
```

## Functional Interfaces

### ObjectStoreManager 

Object Store manger creates, loads and deletes Object Stores

```go
type ObjectStoreManager interface {
    // ObjectStore will lookup and bind to an existing object store instance.
    ObjectStore(bucket string) (ObjectStore, error)
    
    // CreateObjectStore will create an object store.
    CreateObjectStore(cfg *ObjectStoreConfig) (ObjectStore, error)
    
    // DeleteObjectStore will delete the underlying stream for the named object.
    DeleteObjectStore(bucket string) error
}
```

### ObjectStore 

Storing large objects efficiently. Please note that anything that is commented as a "convenience function"
is recommended but optional if it does not make sense for the client language.

```go
type ObjectStore interface {
    // Put will place the contents from the reader into a new object.
    Put(obj *ObjectMeta, reader io.Reader, opts ...ObjectOpt) (*ObjectInfo, error)

    // Get will pull the named object from the object store.
    Get(name string, opts ...ObjectOpt) (ObjectResult, error)

    // PutBytes is convenience function to put a byte slice into this object store.
    PutBytes(name string, data []byte, opts ...ObjectOpt) (*ObjectInfo, error)
    
    // GetBytes is a convenience function to pull an object from this object store and return it as a byte slice.
    GetBytes(name string, opts ...ObjectOpt) ([]byte, error)
    
    // PutBytes is convenience function to put a string into this object store.
    PutString(name string, data string, opts ...ObjectOpt) (*ObjectInfo, error)
    
    // GetString is a convenience function to pull an object from this object store and return it as a string.
    GetString(name string, opts ...ObjectOpt) (string, error)
    
    // PutFile is convenience function to put a file into this object store.
    PutFile(file string, opts ...ObjectOpt) (*ObjectInfo, error)
    
    // GetFile is a convenience function to pull an object from this object store and place it in a file.
    GetFile(name, file string, opts ...ObjectOpt) error
    
    // GetInfo will retrieve the current information for the object.
    GetInfo(name string) (*ObjectInfo, error)
    
    // UpdateMeta will update the meta data for the object.
    // It is an error to update meta data for a deleted object.
    // It is an error to change the name to that of an existing object. 
    UpdateMeta(name string, meta *ObjectMeta) error
    
    // Delete will delete the named object.
    Delete(name string) error
    
    // AddLink will add a link to another object into this object store.
    // It is an error to link to a deleted object.
    // It is an error to link to a link.
    // It is an error to name the link to that of an existing object. 
    // Use UpdateMeta to change the name of a link. 
    AddLink(name string, obj *ObjectInfo) (*ObjectInfo, error)
    
    // AddBucketLink will add a link to another object store.
    // It is an error to name the link to that of an existing object. 
    // Use UpdateMeta to change the name of a link. 
    AddBucketLink(name string, bucket ObjectStore) (*ObjectInfo, error)
    
    // Seal will seal the object store, no further modifications will be allowed.
    Seal() error
    
    // Watch for changes in the underlying store and receive meta information updates.
    Watch(opts ...WatchOpt) (ObjectWatcher, error)
    
    // List will list all the objects in this store.
    List(opts ...WatchOpt) ([]*ObjectInfo, error)
    
    // Status retrieves run-time status about the backing store of the bucket.
    Status() (ObjectStoreStatus, error)
}
```
