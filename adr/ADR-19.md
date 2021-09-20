# JetStream Automatic Status Management

|Metadata|Value|
|--------|-----|
|Date    |2021-09-20|
|Author  |@scottf|
|Status  |Partially Implemented|
|Tags    |jetstream, client|

## Context

This document attempts to describe the Automatic Status Management (ASM) tasks to be performed by the client between receipt of messages and hand-off to the client user.

ASM should be the default client behavior. Optionally, clients can allow users to choose to handle the status messages themself.

## General

Intercept all status messages, never pass to user. If there is some exceptional reason to notify or escalate some state to the user, user a language specific paradigm such as an exception.

## Flow Control and Heartbeat

1. Automatically respond to flow control messages as soon as it is observed in the normal processing of next message. Escalate to user when unable to respond to the flow control.
1. Automatically respond to heartbeats with `Nats-Consumer-Stalled` header. Escalate to user when unable to respond.
1. Monitor JS messages and heartbeats for stream and consumer sequence meta data and escalate to user when a gap is detected.
1. Have an alarm timer that escalates an issue to the user when heartbeats have been missed. Optionally provide a way to specify the period and threshold for an alarm. Default period should equal the heartbeat time and default threshold should be 3 (???)

## Pull

1. Automatically capture 404 and 408 statuses. Those messages are not considered in fetch or iterate counts

## Consequences

Existing client behavior may be released that the default or only behavior is to pass status messages to the user. 
Changning to ASM as the default, while not technically breaking, should be documented in release notes and readmes.
Providing the option for the user to handle the statuses themself would alleviate breaking existing clients.
