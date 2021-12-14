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

## Naming Specification

Protocol Naming Conventions are fully defined in [ADR-6](ADR-6.md)

### Object Store
The object store name or bucket name (`os-bucket-name`) will be used to formulate a stream name and is specified as: `restricted-term` or 1 or more of `A-Z, a-z, 0-9, dash, underscore`

### Objects
An individual object name is not restricted. It is base64 encoded to form the (`os-object-name`).

### Chunk Ids
Chunk ids (`chunk-id`) should be a nuid.

### Component Templates

| Component | Template |
| --- | --- |
| Stream Name | `OBJ_<os-bucket-name>` |
| Object Info Stream subject | `$O.<os-bucket-name>.M.>` |
| Chunk Stream subject | `$O.<os-bucket-name>.C.>` |
| Object Info message subject | `$O.<os-bucket-name>.M.<os-object-name>` |
| Chunk message subject | `$O.<os-bucket-name>.C.<chunk-id>` |

### Default Settings

Default settings can be overridden on a per object basis. 

| Setting | Value | Notes |
| --- | --- | --- |
| Chunk Size | 128k (128 * 1024) | Clients may tune this as appropriate. |

## Basic Design

- Object store or bucket is backed by a stream
- Multiple objects can be placed in each bucket
- Object Info is stored as json in the payload of the message on the Object Info message subject. 
- The Object Info subject is always rolled up (per subject)
- Object chunks are stored as the payload of messages on the Chunk message subject

## Structures

### ObjectStoreConfig

The object store config is the basis for the stream configuration.
```go
type ObjectStoreConfig struct {
    Bucket      string
    Description string
    TTL         time.Duration
    Storage     StorageType
    Replicas    int
}
```

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
    UpdateMeta(name string, meta *ObjectMeta) error
    
    // Delete will delete the named object.
    Delete(name string) error
    
    // AddLink will add a link to another object into this object store.
    AddLink(name string, obj *ObjectInfo) (*ObjectInfo, error)
    
    // AddBucketLink will add a link to another object store.
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
