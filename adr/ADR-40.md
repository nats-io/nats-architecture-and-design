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

## Options

#### Count

The number of responses to allow.

* Responses are accepted until the count is reached.
* Must be used in combination with a time as the count may never be reached.

#### Sentinel

A sentinel is a message with an empty payload. 

* Must be used in combination with a time as a sentinel may never be sent.

#### Max Time

The amount of time to wait for responses.

* Responses are accepted until the max time is reached.

#### First Max / Gap Time

A combination time...

* The first response must be received by the first max time or the request expires.
* Subsequent messages must be received in the gap time, the time since the last message was received or the request expires.
* Each time a message is received the gap timer is reset. 

### Combinations
* Sentinel can be used with any other option.
* Both time options can be used together.
