# Request Many

| Metadata | Value                 |
|----------|-----------------------|
| Date     | 2023-06-26            |
| Author   | @aricart, @scottf,   @Jarema     |
| Status   | Partially Implemented |
| Tags     | client                |

## Problem Statement
Have the client support receiving multiple replies from a single request, instead of limiting the client to the first reply.

## Design

Making the request and handling multiple replies is straightforward. Much like a simplified fetch, the developer
will provide some basic strategy information that tell how many messages to wait for and how long to wait for those messages.

* The client doesn't assume success or failure - only that it might receive messages.
The various options are there to short circuit the length of the wait.
* The Subsequent Max value allows for waiting for the service with the slowest latency. (scatter gather)
* The message count allows for waiting for some count of messages or a timer (scatter gather)

## Config

#### Timeout

**Default: same as default for standard request or 1 second**
If client supports global timeout config, it should be reused as a default here.

The maximum amount of time to wait for responses.
* Responses are accepted until the max time is reached.

#### Muxing
By default, request many does use client request muxer.

### Strategies

#### None
Only timeout applies

#### Interval / Max Jitter
**Default: disabled**

Max interval that can be encountered between messages.
If interval between any messages is larger than set, client will not wait for more messages.

#### Count
**Default: No default. Count is required to use this strategy**

The number of responses to allow.

* Responses are accepted until the count is reached.
* Must be used in combination with a time as the count may never be reached.

## Regarding Time
It's possible that there is a connectivity issue that prevents messages from reaching the requestor so
a request timeout, versus serving the responses being terminated by meeting a time condition.
It would be useful to be able to differentiate between these, but this may not be possible. Do the best you can.

## Additional Discussion
Is there some level of implementation that should be considered as basic core behavior, such as the most simple multiple reply with a max wait?
Or should the entire implementation be dealt with like an add-on, similar to KV, OS

## Beyond Scope, for future consideration

#### Sentinel strategy

A sentinel is a message with an empty payload.

* The sentinel strategy allows for waiting for chunked data,
for instance a message that represents a large file that is larger than the max payload.
* It's up to the developer to understand the data, not the client's responsibility.
    * For ordering for the chunks, maybe use a header.
    * If any chunks were missing, re-issue the request indicating the missing chunk ids.
* Must be used in combination with a time as a sentinel may never be sent or missed.
