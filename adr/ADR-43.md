# Managed Async Publisher

|Metadata| Value                 |
|--------|-----------------------|
|Date    | 2024-05-21            |
|Author  | @scottf               |
|Status  | Partially Implemented |
|Tags    | client                |

## Overview

This document describes a managed async publish utility.

## Publisher

#### Required Properties

* JetStream context on which to publish

#### Optional Properties
| Property                                    | Description                                                                                                                                                                                      |
|---------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| String idPrefix                             | used to make unique identifiers around each message. Defaults to a NUID                                                                                                                          |
| int maxInFlight                             | no more than this number of messages can be waiting for publish ack. Defaults to 50.                                                                                                             |
| int refillAllowedAt                         | if the queue size reaches maxInFlight, a hold is placed so no more messages can be published until the in flight queue is this amount of less, at which time the hold is removed. Defaults to 0. |
| RetryConfig retryConfig                     | if the user wants to publish with retries, they must supply a config, otherwise the publish will be attempted only once.                                                                         |
| long pollTime                               | the amount of time in ms to poll any given queue. Ensures polling doesn't block indefinitely. Defaults to 100ms                                                                                  |
| long holdPauseTime                          | the amount of time in ms to pause between checks when hold is on. Defaults to 100ms                                                                                                              |
| long waitTimeout                            | the timeout when waiting for a publish to be acknowledged. Defaults to 5000ms                                                                                                                    |
| PublisherListener publisherListener         | a callback with the following methods, see description of Flight later                                                                                                                           |


## PublisherListener Interface

The callback interface for the user to get information about the publish workflow

| Method                                      | Description                                                        |
|---------------------------------------------|--------------------------------------------------------------------|
| void published(Flight flight)               | the flight is ready when the message is published                  |
| void acked(Flight flight);                  | the publish ack was received                                       |
| void completedExceptionally(Flight flight); | the publish exceptioned, including the lower level request timeout |
| void timeout(Flight flight)                 | the ack was not returned in time based on wait timeout             |

## Flight structure

An object that holds state of the message as it makes its way through the workflow

| Property                                            | Description                                                                     |
|-----------------------------------------------------|---------------------------------------------------------------------------------|
| long getPublishTime()                               | the time when the actual js publish was executed. Used for timeout determination |
| CompletableFuture<PublishAck> getPublishAckFuture() | the future for the publish ack                                                  |
| String getId()                                      | `<idPrefix>-<number>` for example                                               |
| String getSubject()                                 | the original message subject                                                    |
| Headers getHeaders()                                | the original message headers                                                    |
| byte[] getBody()                                    | the original message body                                                       |
| PublishOptions getOptions()                         | the original message publish options (i.e. expectations)                        |


## Behavior

| Name          | Description                                                                                                                                                                                               |
|---------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| publishRunner | loop that reads from the queue of messages added by the user                                                                                                                                              |
| flightsRunner | loop that checks the status of publish acks                                                                                                                                                               |
| start         | start the runners with individual threads. Designed to allow user to start the runners on threads they provide themselves.                                                                                |
| stop          | stop the runners before the next iteration                                                                                                                                                                |
| drain         | prevent any messages being added to the queue. Workflow stops when all messages are published then acknowledged/exceptioned/time-out                                                                      |
| publishAsync  | parallel api to JetStream publishAsync api. All return a CompletableFuture<Flight> that is complete once the message is actually published. Can be ignored in place of PublisherListener.publish callback |


## Publish Runner Pseudo Code
```
while keepGoing flag
  if (not in hold pattern)
      check the user's queue using pollTime
      if there is something...
        if it's the drain maker, we are done
        else
          publish async (with retry config if provided)
          notify listener to indicate published
          if in flight queue has reached maxInFlight put hold on
  else in holding pattern
      sleep holdPauseTime
```

## Flights Runner Pseudo Code:
```
while keepGoing flag
  check the in flight queue using pollTime
      if there is something
        if been asked to drain && user queue is empty and in flights queue is empty, we are done
        else
          if the flight's publish ack future is done
              if the future is complete and publish ack was received
                notify listener to indicate the publish was acked
              else if the future was completed with an exception
                notify listener to indicate the publish completed exceptionally
              else if the wait timeout has been exceeded
                notify listener to indicate the publish timed out
                complete the future exceptionally so if the user is waiting on the future instaed of the callback they can see it
              else put it back in the queue for later
        if in flight queue has equal to or less than the refillAllowedAt threshold make remove the hold
```