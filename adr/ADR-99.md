# Connect/Reconnect Logic

|Metadata| Value                 |
|--------|-----------------------|
|Date    | 2022-02-15            |
|Author  | @scottf, @alberto     |
|Status  | Partially Implemented |
|Tags    | client                |

## Context

This document describes the connect and reconnect behavior that the clients should implement, as well as some
optional behavior.

## Options

| Option                       | Default   | Description                                                 |
|------------------------------|-----------|-------------------------------------------------------------|
| MaxReconnect                 | 60        | The number of times each possible entry can be tried        |
| Reconnect Wait               | 2 seconds | ???                                                         |
| Reconnect Jitter             | 100 ms    | ???                                                         |
| Secure Reconnect Jitter      | 1000 ms   | ???                                                         |
| Connection Timeout           | 2 seconds | ???                                                         |
| Reconnect Delay Handler      | internal  | ???                                                         |
| Reconnect on Connect         | true      | Whether to do reconnect logic if the initial connect fails  |
| Reconnect forever on Connect | false     | Whether max reconnect applies during Reconnect on Connect   |  
