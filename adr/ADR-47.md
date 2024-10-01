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
The wait for the first message is always made with the total timeout since at least one message must come in within the total time.

* Always used
* Defaults to the connection or system request timeout.
* A user could provide a very large value, there has been no discussion of validating. Might be used in a sentinel case or where a user can cancel. Not recommended? Should be discouraged?

### Stall timer

The amount time before to wait for subsequent messages. 
Considered "stalled" if this timeout is reached, the request is complete.

* Optional
* Less than 1 or greater than or equal to the total timeout is the same as not supplied.
* Defaults to not applicable.
* When supplied, subsequent waits are the lesser of the stall time or the calculated remaining time. 
This allows the total timeout to be honored and for the stall to not extend the loop past the total timeout.

### Max messages

The maximum number of messages to wait for. 
* Optional
* If this number of messages is received, the request is complete.
* If this number is supplied and both total timeout and stall timeout are not set, total timeout defaults to the connection or system timeout.

### Sentinel

While processing the messages, the user should have the ability to indicate that it no longer wants to receive any more messages.
* Optional
* Language specific implementation
* If this number is supplied and both total timeout and stall timeout are not set, total timeout defaults to the connection or system timeout.

## Notes

### Message Handling

Each client must determine how to give messages to the user.
* They could all be collected and given at once.
* They could be put in an iterator, queue, channel, etc.
* A callback could be made.

### End of Data

The developer should notify the user when the request has stopped processing
for completion, sentinel or error conditions (but maybe not if the user cancelled or terminates.)
Implementation is language specific based on control flow.

Examples would be sending a marker of some sort to a queue, terminating an iterator, returning a collection, erroring.

### Status Messages / Server Errors

If a status (like a 503) or an error comes in place of a user message, this is terminal.
This is probably useful information for the user and can be conveyed as part of the end of data.

#### Callback timing

If callbacks are made in a blocking fashion, the client must account for the time it takes for the user to process the message.

### Sentinel

If the client supports a sentinel with a callback predicate that accepts the message and returns a boolean, 
a return of true would mean continue to process and false would mean stop processing.

### Cancelling

If possible, the user should be able to cancel the request. This is not the sentinel.

## Disconnection

It's possible that there is a connectivity issue that prevents messages from reaching the requester,
It might be difficult to differentiate that timeout from a total or stall timeout. 
If possible to know the difference, this could be conveyed as part of the end of data. 

## Strategies
It's acceptable to make "strategies" via enum / api / helpers / builders / whatever.
Strategies are just pre-canned configurations.

#### Examples
Timeout - this is the default strategy where on the total timeout is used.
Max Responses plus Total Time - the user wants a certain number of responses, but they also don't want to wait too long
Stall Only - this strategy, only a stall is used. Problematic if stall is less then the time it takes to get the very first message
Max Responses Only - wait forever for a number of responses. Problematic to have no total wait time.
