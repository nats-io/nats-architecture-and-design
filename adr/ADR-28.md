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


