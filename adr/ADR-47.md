# Request Many

| Metadata | Value                      |
|----------|----------------------------|
| Date     | 2024-09-26                 |
| Author   | @aricart, @scottf, @Jarema |
| Status   | Partially Implemented      |
| Tags     | client                     |

| Revision | Date       | Author    | Info                    |
|----------|------------|-----------|-------------------------|
| 1        | 2024-09-26 | @scottf   | Document Initial Design |

## Problem Statement
Have the client support receiving multiple replies from a single request, instead of limiting the client to the first reply.

## Basic Design

The user can provide some configuration controlling how and how long to wait for messages.
The client handles the requests and subscriptions and provides the messages to the user.

* The client doesn't assume success or failure - only that it might receive messages.
* The various configuration options are there to manage and short circuit the length of the wait, 
and provide the user the ability to directly stop the processing.
* Request Many is not a recoverable operation, but it could be wrapped in a retry pattern.

## Config

### Total timeout

The maximum amount of time to wait for responses. When the time is expired, the process is complete.
The wait for the first message is always made with the total timeout, since at least one message must come in within the total time.

* Always used
* Defaults to the connection or system request timeout.
* A user could provide a very large value, there has been no discussion of validating. Might be used in a sentinel case or where a user can cancel. Not recommended? Should be discouraged?

### Stall timer

The amount time before to wait for subsequent messages. 
Considered "stalled" if this timeout is reached, the request is complete.

* Optional
* Less than 1 or greater than or equal to the total timeout is the same as not supplied.

### Max messages

The maximum number of messages to wait for. 
* Optional
* If this number of messages is received, the request is complete.

### Sentinel

While processing the messages, the user should have the ability to indicate that it no longer wants to receive any more messages.
* Optional
* Language specific implementation   

## Notes

### Message Handling

Each client must determine how to give messages to the user.
* They could all be collected and given at once.
* They could be put in an iterator, queue, channel, etc.
* A callback could be made.

### End of Data

The developer should notify the user when the request has stopped processing
for completion, sentinel or error conditions but maybe not on if the user cancelled or terminates.
Implementation is language specific based on control flow.

Examples would be sending a marker of some sort to a queue, terminating an iterator, returning a collection, erroring.

### Status Messages / Server Errors

If a status or error comes in place of a user message, this is terminal.
This is probably useful information for the user and can be conveyed as part of the end of data.

#### Callback timing

If callbacks are made in a blocking fashion, the client must account for the time it takes for the user to process the message.

### Sentinel

If the client supports a sentinel, for instance with a callback predicate that accepts the message and returns a boolean, 
a return of true would mean continue to process and false would mean stop processing.

### Cancelling

Wherever possible, the user should be able to cancel the request. This is not the sentinel.

## Disconnection

It's possible that there is a connectivity issue that prevents messages from reaching the requestor,
so it might be difficult to differentiate from a total or stall timeout. 
If possible and useful in the client, this can be conveyed as part of the end of data. 

## Strategies
It's acceptable to make "strategies" via enum / api / helpers / builders / whatever.
Strategies are just pre-canned configurations.

**Strategies are not specified yet, this is just an example**

Here is an example from Javascript. Jitter was the original term for stall.

```js
export enum RequestStrategy {
  Timer = "timer",
  Count = "count",
  JitterTimer = "jitterTimer",
  SentinelMsg = "sentinelMsg",
}
```
