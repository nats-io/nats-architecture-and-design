# JetStream RePublish 

| Metadata | Value                   |
|----------|-------------------------|
| Date     | 2022-07-08              |
| Author   | @derekcollison, @tbeets |
| Status   | `Implemented`           |
| Tags     | server                  |

## Context and Problem Statement

In some use cases it is useful for a subscriber to monitor messages that have been ingested by a stream (captured to
store) without incurring the overhead of defining and using a JS Consumer on the stream.

Such use cases include (but are not limited to):

* Lightweight stream publish monitors (such as a dashboard) that don't require the overhead of At-Least-Once delivery
* No side-effect WorkQueue and Interest-Based stream publish monitoring
* KV or Object Store update events as an alternative to watches, e.g. an option for cache invalidation

## Design

If stream _RePublish_ option is configured, a stream will evaluate each published message (that it ingests) against
a `RePublish Source` subject filter. Upon match, the stream will re-publish the message (with special message headers 
as below) to a new `RePublish Subject` derived through a destination subject transformation.

> Re-publish occurs only after the original published message is ingested in the stream (with quorum for R>1 streams) and is
_At-Most-Once_ QoS.

### RePublish Configuration Option

The RePublish option "republish" consists of three configuration fields:

| Field        | Description                                 | JSON         | Required | Default |
|--------------|---------------------------------------------|--------------|----------|---------|
| Source       | Published Subject-matching filter           | src          | N        | \>      |
| Destination  | RePublish Subject template                  | dest         | Y        |         | 
| Headers Only | Whether to RePublish only headers (no body) | headers_only | N        | false   |

The following validation rules for RePublish option apply:

* Source and Destination MUST be in valid subscription format and MAY have the `>` wildcard as last token 
* Source MUST be a valid subset of the stream's subject space (aggregate of stream's subject filters). A single token
as `>` wildcard matches any stream-ingested subject
* Destination MUST have at least 1 non-wildcard token
* If Source has at least 1 non-wildcard token, Destination must have an identical number of non-wildcard tokens as Source
* If Source includes a `>` wildcard, Destination must also include a `>` wildcard

Here is an example of a stream configuration with the RePublish option specified:
```text
{
	"name": "Stream1",
	"subjects": [
		"one.>",
		"four.>"
	],
	"republish": {
        "src": "one.>",
        "dest": "uno.>",
        "headers_only": "false"
	},
	"retention": "limits",
	... omitted ...
}
```

> RePublish option configuration MAY be edited after stream creation.

### RePublish Transform

RePublish Destination, taken together with RePublish Source, form a valid subject token transform (positional) 
rule. The resulting transform is applied to each ingested message (that matches Source configuration) to determine the 
the concrete RePublish Subject.

If the RePublish Source contains a single token that is the `>` wildcard (which is the default if no RePublish Source
specified), then the RePublish Destination essentially forms a "prefix token(s)" transform. The RePublished Subject will
be the same as Published Subject with one (or more) prefix tokens added to the subject hierarchy. 

| Stream Subject Scope | RePublish Source | RePublish Destination | Published Subject | RePublished Subject   |
|:---------------------|------------------|-----------------------|:------------------|-----------------------|
| one.>, four.>        | \>               | uno.>                 | one.two.three     | uno.one.two.three     |
|                      |                  |                       | four.five.six     | uno.four.five.six     | 
|                      |                  | uno.dos.>             | one.two.three     | uno.dos.one.two.three | 
|                      |                  |                       | four.five.six     | uno.dos.four.five.six | 
| one.>, four.>        | one.>            | uno.>                 | one.two.three     | uno.two.three         |
|                      |                  |                       | four.five.six     | _no msg_              | 
| one.>, four.>        | one.two.>        | uno.dos.>             | one.two.three     | uno.dos.three         |
|                      |                  |                       | four.five.six     | _no msg_              | 
| one, four            | one              | uno                   | one               | uno                   |
|                      |                  |                       | four              | _no msg_              | 

> The RePublish option MAY NOT be configured such that a RePublished Subject matches the stream's subject scope.
> This is to avoid a publish loop.
 
### RePublish Headers

Each RePublished Message will have the following message headers:

| Header             | Value Description                                                                                          |
|--------------------|------------------------------------------------------------------------------------------------------------|
| Nats-Stream        | Stream name (in scope to stream's account)                                                                 |
| Nats-Subject       | Message's original subject as ingested into stream                                                         |
| Nats-Sequence      | This message's stream sequence id                                                                          |
| Nats-Last-Sequence | The stream sequence id of the last message ingested to the same original subject (or 0 if none or deleted) |

If headers-only RePublished Message, also:

| Header        | Value Description                       |
|---------------|-----------------------------------------|
| Nats-Msg-Size | The size in bytes of the message's body |

> Any application headers on the original Published Message will be preserved in the RePublished message.
