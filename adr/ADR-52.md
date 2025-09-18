# No Headers support for Direct Get

| Metadata | Value      |
|----------|------------|
| Date     | 2025-06-19 |
| Author   | @ripienaar |
| Status   | Deprecated |
| Tags     | deprecated |

# Context

Often the only part of a message users care for is the body, a good example is counters introduced in ADR-49 where there is a data section in the body and a control section in the headers. For clients that only care for the current count there is no need to even download the headers from the server.

We support this feature in both Direct Get and the Message Get API.

# API Changes

We add an option to the `JSApiMsgGetRequest` structure as below:

```go
type JSApiMsgGetRequest struct {
	// ....

	// NoHeaders disable sending any headers with the body payload
	NoHeaders bool `json:"no_hdr,omitempty"`
}`
```

When set the server will simply send the body without any headers, headers like `Nats-Sequence` will also be unset.  

This also applies to batch direct gets, where no messages in the batch will have headers. However the final zero payload message will still have the usual batch control headers.