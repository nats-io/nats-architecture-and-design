# JetStream Direct Get 

| Metadata | Value                                         |
|----------|-----------------------------------------------|
| Date     | 2022-08-03                                    |
| Author   | @mh, @ivan, @derekcollison, @alberto, @tbeets |
| Status   | Implemented                                   |
| Tags     | jetstream, client, server                     |

## Context and motivation 

In initial design of JetStream, reading a message from a stream _directly_, i.e. _not_ via JS Consumer delivery, was 
thought to be an administrative function and API. All such reads are routed to the current stream leader (and its 
underlying stream store) and read calls are tracked as administrative and incur tracking overhead.

In some use cases, notably key _get_ on a stream that is a KV materialized view, it is desirable to both
spread message read load to multiple servers (each accessing a message store local to them) and bypass
administrative API overhead.

### Feature: Direct Get

The JetStream _Direct Get_ feature enables all stream peers (R>1), not just the stream leader, to respond 
to stream read calls as a service responder _queue group_. The responder sources its local message
store. With Direct Get the number of servers eligible to respond to read requests is the same as the replica 
count of the stream.

### Extended feature: MIRROR Direct Get responders

For streams that are _Direct Get_ enabled and are also an upstream source of a MIRROR stream, the mirror's peer
servers will also participate in the responder queue group for Direct Get calls _to the upstream_. In this manner,
message read can be spread to many additional servers.  

As mirrors need not be in the same cluster as the upstream, servers that respond to Direct Get requests to the 
upstream can be strategically placed for client latency-reduction, e.g. different geographic locations serving distributed
clients. Also, read availability can be enhanced as mirrors may be available to clients when the upstream is offline.

###### A note on read-after-write coherency

The existing Get API `$JS.API.STREAM.MSG.GET.<stream>` provides read-after-write coherency by routing requests to a 
stream's current peer leader (R>1) or single server (R=1). A client that publishes a message to stream (with ACK) is 
assured that a subsequent call to the Get API will return that message as the read will go a server that defines 
_most current_. 

In contrast, _Direct Get_ does not assure read-after-write coherency as responders may be non-leader stream servers 
(that may not have yet applied the latest consensus writes) or MIRROR downstream servers that have not yet _consumed_ 
the latest consensus writes from upstream.

## Implementation

### Stream property: Allow Direct 

- Stream configuration adds a new Allow Direct boolean property: `allow_direct`
- Allow Direct is always set to `true` by the server when maximum messages per subject, `max_msgs_per_subject`, is configured to be > 0 (a max limit is specified)
- If the user passes Allow Direct explicitly in stream create or edit request, the value will be overriden
based on `max_msgs_per_subject`

> Allow Direct is set automatically based on the inferred use case of the stream. Maximum messages per subject is a
tell-tale of a stream that is a KV bucket.

### Direct Get API 

When Allow Direct is true, each of the stream's servers configures a responder and subscribes to 
`$JS.API.DIRECT.GET.<stream>` with fixed queue group `_sys_`. 

> Note: If Allow Direct is false, there will be no responder at the Direct Get API for a stream. Clients that make
> requests will receive no reply message and will time out.

###### Request

Clients may make requests with the same payload as the Get message API populating the following server struct:

 ```text
Seq      uint64 `json:"seq,omitempty"`
LastFor  string `json:"last_by_subj,omitempty"`
NextFor  string `json:"next_by_subj,omitempty"`
Batch    int `json:"batch,omitempty"`
MaxBytes int `json:"max_bytes,omitempty"`
```

Example request payloads:

* `{seq: number}` - get a message by sequence
* `{last_by_subj: string}` - get the last message having the subject
* `{next_by_subj: string}` - get the first message (lowest seq) having the specified subject
* `{seq: number, next_by_subj: string}` - get the first message with a seq >= to the input seq that has the specified subject
* `{seq: number, batch: number, next_by_subj: string}` - gets up to batch number of messages >= than seq that has specified subject
* `{seq: number, batch: number, next_by_subj: string, max_bytes: number}` - as above but limited to a maximum size of messages received in bytes

#### Subject-Appended Direct Get API

The purpose of this form is so that environments may choose to apply subject-based interest restrictions (user permissions
within an account and/or cross-account export/import grants) such that only specific
subjects in stream may be read (vs any subject in the stream).

When Allow Direct is true, each of the stream's servers will also subscribe to `$JS.API.DIRECT.GET.<stream>.>` with
fixed queue group `_sys_`.  Requests to this API endpoint will be interpreted as a shortcut for `last_by_subj` request
where `last_by_subj` is derived by the token (or series of tokens) following the stream name rather than request payload.

It is an error (408) if a client calls Subject-Appended Direct Get and includes a request payload.

#### Batched requests

Using the `batch` and `max_bytes` keys one can request multiple messages in a single API call.

The server will send multiple messages without any flow control to the reply subject, it will send up to `max_bytes` messages.  When `max_bytes` is unset the server will use the `max_pending` configuration setting or the server default (currently 64MB)

After the batch is sent a zero length payload message will be sent with the `Nats-Pending-Messages` header set that clients can use to determine if further batch calls are needed. Additionally the `Nats-Last-Sequence` will hold the sequence of the last message sent. It would also have the `Status` header set to `204` with the `Description` header being `EOB`.

When requests are made against servers that do not support `batch` the first response will be received and nothing will follow. Old servers can be detected by the absence of the `Nats-Num-Pending` header in the first reply.

#### Response Format 

Responses may include these status codes:

- `204` indicates the the end of a batch of messages, the description header would have value `EOB`
- `404` if the request is valid but no matching message found in stream 
- `408` if the request is empty or invalid

> Error code is returned as a header, e.g. `NATS/1.0 408 Bad Request`. Success returned as `NATS/1.0` with no code.

Direct Get replies contain the message along with the following message headers:

- `Nats-Stream`: stream name
- `Nats-Sequence`: message sequence number 
- `Nats-Time-Stamp`: message publish timestamp
- `Nats-Subject`: message subject
- `Nats-Num-Pending`: when batched, the number of messages left in the stream matching the batch parameters
- `Nats-Last-Sequence`: when batched, the stream sequence of the previous message
- `Nats-Pending-Messages`: the final nil-body message for a batch would have this set indicating how many messages are left matching the request

> A _regular_ (not JSON-encoded) NATS message is returned (from the stream store).

## Example calls

#### Direct Get (last_by_subj)

Request:
```text
PUB $JS.API.DIRECT.GET.KV_mykv1 _INBOX.6ZtubEqXICZLn7AI4uEiPQ.MmLZadFE 35
{"last_by_subj":"$KV.mykv1.mykey1"}
```

Reply:
```text
HMSG _INBOX.6ZtubEqXICZLn7AI4uEiPQ.MmLZadFE 1 143 148
NATS/1.0
Nats-Stream: KV_mykv1
Nats-Subject: $KV.mykv1.mykey1
Nats-Sequence: 1
Nats-Time-Stamp: 2022-08-06 00:29:27.587861324 +0000 UTC

hello
```

#### Direct Get (next_by_sub starting at seq)

Request:
```text
PUB $JS.API.DIRECT.GET.KV_mykv1 _INBOX.pOdFG6hX5uAxqs0JWrAwoY.XJcZuCY4 44
{"seq":1, "next_by_subj":"$KV.mykv1.mykey2"}
```

Reply:
```text
HMSG _INBOX.pOdFG6hX5uAxqs0JWrAwoY.XJcZuCY4 1 143 150
NATS/1.0
Nats-Stream: KV_mykv1
Nats-Subject: $KV.mykv1.mykey2
Nats-Sequence: 2
Nats-Time-Stamp: 2022-08-07 06:47:46.610665303 +0000 UTC

goodbye
```

#### Subject-Appended Direct Get
Request:
```text
PUB $JS.API.DIRECT.GET.KV_mykv1.$KV.mykv1.mykey1 _INBOX.qoxA09fQH9fZqNNqrGNPLg.DYPxJr9Y 0`
```

Reply:
```text
HMSG _INBOX.qoxA09fQH9fZqNNqrGNPLg.DYPxJr9Y 1 143 148
NATS/1.0
Nats-Stream: KV_mykv1
Nats-Subject: $KV.mykv1.mykey1
Nats-Sequence: 1
Nats-Time-Stamp: 2022-08-06 00:29:27.587861324 +0000 UTC

hello
```

#### Illegal Subject-Appended Direct Get
Request:
```text
PUB $JS.API.DIRECT.GET.KV_mykv1.$KV.mykv1.mykey2 _INBOX.4NogKOPzKEWTqhf4hFIUJV.yo2tE1ep 44
{"seq":1, "next_by_subj":"$KV.mykv1.mykey2"}
```

Reply:
```text
HMSG _INBOX.4NogKOPzKEWTqhf4hFIUJV.yo2tE1ep 1 28 28
NATS/1.0 408 Bad Request
```
