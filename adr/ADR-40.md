# Request Many

| Metadata | Value                 |
|----------|-----------------------|
| Date     | 2023-06-26            |
| Author   | @aricart, @scottf     |
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

## Options

#### Max Wait

The maximum amount of time to wait for responses.

* Responses are accepted until the max time is reached.

#### First Max / Subsequent Max

A combination time...

* The first response must be received by the first max time or the request terminates.
* Subsequent messages must be received in the gap time, the time since the last message was received or the request expires.
* Each time a message is received the gap timer is reset.

#### Count

The number of responses to allow.

* Responses are accepted until the count is reached.
* Must be used in combination with a time as the count may never be reached.

#### Sentinel

A sentinel is a message with an empty payload.  

* The sentinel strategy allows for waiting for chunked data, 
for instance a message that represents a large file that is larger than the max payload.  
* It's up to the developer to understand the data, not the client's responsibility. 
    * For ordering for the chunks, maybe use a header.
    * If any chunks were missing, re-issue the request indicating the missing chunk ids.
* Must be used in combination with a time as a sentinel may never be sent or missed.

## Regarding Time

It's possible that there is a connectivity issue that prevents messages from reaching the requestor so 
a request timeout, versus serving the responses being terminated by meeting a time condition.
It would be useful to be able to differentiate between these, but this may not be possible. Do the best you can.

## Defaults
Much like simplification, we could provide common defaults across clients around time options. 
* The subsequent max seems like a good candidate for this, for instance 100ms?
* The max wait and first max might do well to default to slightly (100ms) longer than the default request or connection timeout.


## Additional Discussion

Is there some level of implementation that should be considered as basic core behavior, such as the most simple multiple reply with a max wait?
Or should the entire implementation be dealt with like an add-on, similar to KV, OS