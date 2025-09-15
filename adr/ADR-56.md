# JetStream Consistency Models

| Metadata | Value                       |
|----------|-----------------------------|
| Date     | 2025-09-12                  |
| Author   | @ripienaar, @MauriceVanVeen |
| Status   | Approved                    |
| Tags     | server, 2.12                |

| Revision | Date       | Author                      | Info                                        |
|----------|------------|-----------------------------|---------------------------------------------|
| 1        | 2025-09-12 | @ripienaar, @MauriceVanVeen | Initial document for R1 `async` consistency |

## Context and Problem Statement

JetStream is a distributed message persistence system and delivers certain promises when handling user data.

This document intends to document the models it support, the promises it makes and how to configure the different models.

> [!NOTE]  
> This document is a living document; at present we will only cover the `async` persistence model with an aim to expand in time
> 

## R1 `async` Write Consistency

The `async` consistency model of a stream will result in asynchronous flushing of data to disk, this result in a significant speed-up as each message will not be written to disk but at the expense of data loss during severe disruptions in power, server or disk subsystems.

If the server is running with `sync: always` set then that setting will be overridden by this setting for the specific stream. It would not be in `sync: always` mode anymore despite the system wide setting.

At the moment this mode cannot support batch publishing at all and any attempt to start a batch against a stream in this mode must fail.

This setting will require API Level 2.

### Implications:

 * The Publish Ack will be sent before the data is known to be written to disk
 * An increased chance of data loss during any disruption to the server

### Configuration:

 * The `WriteConsistency` key should be unset or `strong` for the default strongest possible consistency level
 * Setting it on anything other than a R1 stream will result in an error
 * Scaling a R1 stream up to greater resiliency levels will fail if the `WriteConsistency` is not set to `async`
 * When the user provides no value for `WriteConsistency` the implied default is `strong` but the server will not set this in the configuration, result of INFO requests will also have it unset
 * Setting `WriteConsistency` to anything other than empty/absent will require API Level 2

