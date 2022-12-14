# Service API

| Metadata | Value                 |
| -------- | --------------------- |
| Date     | 2022-11-23            |
| Author   | @aricart              |
| Status   | Partially Implemented |
| Tags     | client                |

## Context and Problem Statement

Simplify the development of NATS micro-services.

The design goal of the API is to reduce development to having a complexity
similar to that of writing a NATS subscription, but then by using simple
configuration, allow the specified metadata to allow for standardization of
discovery and observability.

## Design

Service configuration relies on the following:

- `name` - really the _kind_ of the service. Shared by all the services that
  have the same name. This `name` can only have
  `A-Z, a-z, 0-9, dash, underscore`.
- `version` - a SemVer string - impl should validate that this is SemVer
- `description` - a human-readable description about the service (optional)
- `schema`: (optional)
  - `request` - a string/url describing the format of the request payload can be
    JSON schema etc.
  - `response` - a string/url describing the format of the response payload can
    be JSON schema etc.
- `statsHandler` - an optional function that returns unknown data that can be
  serialized as JSON. The handler will be provided the endpoint for which it is
  building a `EndpointStats`
- `endpoint` - a subject and a handler (effectively equivalent to a NATS
  subscription)

All services are created using a function called `addService()` where the above
options are passed. The function returns an object/struct that represents the
service. At a minimum the service is expected to offer functions/methods that
allow:

- a `stop(error?)` function that allows user code to stop the service.
  Optionally this function should allow for an optional error. Stop should
  always drain its service subscriptions.
- `reset()` to reset any tracked metrics
- `info()` returns the [`ServiceInfo`](#INFO)
- `stats()` to return the stats of the service
- A callback handler or promise where the framework can notify when the service
  has stopped. Note that this is independent of the NATS connection, and it
  should be possible to run multiple services under a single connection.

On startup a service is assigned an unique `id`. This `id` is used to
distinguish different instances of the service and allow for a specific instance
of the service to be addressed.

### Discovery and Status

Using the specified `name` and automatically generated `id` the service will
automatically create a subscription to handle discovery and monitoring requests.

The subject for discovery and requests is always composed of all capital
letters. Prefixed by `$SRV`. Note that this prefix needs to be overridable much
in the way as we do for `$JS`, in order to enable targetting tools to work
across accounts.

The initial _verbs_ supported by the service include:

- `PING`
- `STATS`
- `INFO`
- `SCHEMA`

Using the above verbs, it becomes possible to build a service subject hierarchy
like:

`$SRV.PING|STATS|INFO|SCHEMA` - pings and retrieves status for all services
`$SRV.PING|STATS|INFO|SCHEMA.<name>` - pings or retrieves status for all
services having the specified name `$SRV.PING|STATS|INFO|SCHEMA.<name>.<id>` -
pings or retrieves status of a particular service instance

Services should respond to:

- All service requests
- All service requests that match their `name`
- All services requests that match their `name` and `id`

### Standard Field

All discovery and status responses contain the following fields:

```typescript
    /**
    * The kind of the service reporting the status
    */
    name: string,
    /**
    * The unique ID of the service reporting the status
    */
    id: string,
    /**
    * The version of the service
    */
    version: string
}
```

### INFO

Returns a JSON having the following structure:

```typescript
{
    name: string,
    id: string,
    version: string,
    /**
    * Description for the service
    */
    description: string,
    /**
     * Version of the service
     */
    version: string,
    /**
     * Subject where the service can be invoked
     */
    subject: string
}
```

All the fields above map 1-1 to the metadata provided when the service was
created. Note that `subject` is the subject that the service is listening for
requests on.

### PING

Returns the following schema (the standard response fields)

```typescript
{
    name: string,
    id: string,
    version: string,
}
```

The intention of `PING` is for clients to calculate RTT to a service and discover
services.

### SCHEMA

Returns a JSON having the following structure. Note that the `schema` struct is
only returned if the `schema` was specified when created.

```typescript
{
    name: string,
    id: string,
    version: string,
    /**
     * The schema specified when the service was created
     */
    schema?: {
        /**
         * A string or URL
         */
        request: string,
        /**
         * A string or URL
         */
        response: string
    };
}
```

### STATS

```typescript
{
    name: string,
    id: string,
    version: string,
    /**
    * The number of requests received by the endpoint
    */
    num_requests: number;
    /**
    * Number of errors that the endpoint has raised
    */
    num_errors: number;
    /**
    * If set, the last error triggered by the endpoint
    */
    last_error?: Error;
    /**
    * A field that can be customized with any data as returned by stats handler see {@link ServiceConfig}
    */
    data?: unknown;
    /**
    * Total processing_time for the service
    */
    processing_time: Nanos;
    /**
    * Average processing_time is the total processing_time divided by the num_requests
    */
    average_processing_time: Nanos;
    /**
    * ISO Date string when the service started
    */
    stared: string
}
```

## Error Handling

Services may communicate request errors back to the client as they see fit, but
to help standardization they also must include the headers: `Nats-Service-Error`
and `Nats-Service-Error-Code`.

`Nats-Service-Error-Code` should be a value that is always safe to parse as a
number. `Nats-Service-Error` should be a string describing the error that could
be shown to the user.

This means that clients making request from the service _must_ check if the
response is an error by looking for these headers. This allows client code to be
fairly standard in terms of handling regardless of additional error handling
conventions.

Service API libraries _must_ provide an error formatting function that users can
use to produce the properly formatted response headers.

## Request Handling

All service request handlers operate under the queue group `q`. This means that
in order to scale up or down all the user needs to do is add or stop services.
Note the name of the queue group is fixed to `q` and cannot be changed otherwise
different implementations on different queue groups will respond to the same
request.

The handler specified by the client to process requests should operate as any
standard subscription handler. This means that no assumption is made on whether
returning from the callback signals that the request is completed. The framework
will dispatch requests as fast as the handler returns.
