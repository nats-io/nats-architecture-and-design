# NATS Client Middleware

| Metadata | Value      |
| -------- | ---------- |
| Date     | 2024-03-01 |
| Author   | @aricart   |
| Status   | `Proposed` |
| Tags     | client     |

## Context and Problem Statement

To consistently pre-process a message has always been possible in NATS provided
the handler had a set of utility functions to do the work. It would useful to
have a standard mechanism or approach to handle such functionality much in the
in the same way as HTTP middleware.

## [Context | References | Prior Work]

HTTP middleware has for a long time been the standard way to pre-process a
request and inject and format data and headers before it is handled by the
intended handler. An initial step which for the NATS client ecosystem is to
define a standard pattern that could be applied to messages as the client
processes.

## Design

There are a number of ways to implement middleware in NATS. Of concern is
actively performing the pre-processing deep in the client library as errors
could be difficult to trace and debug, specially when the processing callback
may not be able to offer an error for the client inspect which results in
support calls when things are not quite right. This will be true of any iterator
that yields messages for a subscription.

## Decision

An initial approach is to simply provide a low impact pipeline that can be used
to pre-process messages. This allows the user to have a standard pattern to
build pipelines, yet keep the implementation executing within the user's
callback or iterator, and thus expose issues in the user's code. It also has the
advantage, that the current API for the clients doesn't need to change.

Future versions of the client could provide better integration, but that will
require modifying or adding API in order to catch errors described above.

## Concept Implementation

The basic idea is simply to have a function that takes as an argument a message
and returns a message (and possibly an error), here's a simple prototype, this
example is in TypeScript and doesn't expose the errors in the form of a
`Promise<Msg>` to avoid complicating non-async handlers. Perhaps in this case a
better alternative is to return a `ResultMsg` which holds a possible error to
indicate a failure.

```typescript
// A pipeline function simply takes a message and returns a message
export type PipelineFn = (msg: Msg) => Msg;

// A pipeline has a function that when called with a message
// invokes all its PipelineFn's in order, and then returns
// the transformed message
export interface Pipelines {
  transform(m: Msg): Msg;
}

// Punting the actual implementation of the Pipeline, here's how
// the client would use such an API:
function addHeader(m: Msg): Msg {
  // this needs to clone the headers
  const h = m.headers || headers();
  h.set("X-Transformed", "yes");
  const mm = new MutableMsg(m);
  mm.headers = h;
  return mm;
}

function reverse(m: Msg): Msg {
  const mm = new MutableMsg(m);
  mm.data = new TextEncoder().encode(m.string().split("").reverse().join(""));
  return mm;
}

const pipeline = new Pipeline(addHeader, reverse);

// Here's a service that echos the request message by
// calling the pipeline to reverse the data and add a header.
// The operator could as easily transformed the message to
// JSON or some other format.
nc.subscribe("q", {
  callback(_, msg) {
    const m = pipeline.transform(msg);
    // javascript doesn't have respondMsg() or publishMsg()
    msg.respond(m.data, { headers: m.headers });
  },
});
```

The implementation of the middleware relies on being able to construct `Msg`
objects that conform to the interface of a NATS message. Some clients, such as
JavaScript don't expose a way of creating a `Msg` object, or to publish a
message with a `Msg` argument, but each client can provide internal
implementations on how to accomplish that.

For clients, like the `go` client, the ability to create a `Msg` has always been
possible, and the API already sports a `PublishMsg()` and `RespondMsg()`.

There are some nuances in the implementation. Most clients try to reduce
allocations and data copies as much as possible. For example the JavaScript
clients simply have a subview of a message from the data read from the socket.
This means that decoding a subject or reading a header, or even extracting the
data doesn't happen until user code invokes such operation.

Pipelines when possible should operate the same way, thus preventing any
unnecessary allocations or copies as much as possible.
