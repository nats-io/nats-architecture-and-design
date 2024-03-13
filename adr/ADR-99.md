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

**Max Reconnect**                
  * The number of times each possible entry can be tried
  * Default: 60 times each item in the server list

**Reconnect Wait**
  * Description
  * Default: 2 seconds

**Reconnect Jitter**             
  * Description
  * Default: 100 ms

**Secure Reconnect Jitter**
  * Description
  * Default: 1000 ms

**Connection Timeout**
  * Description
  * Default: 2 seconds

**Reconnect Delay Handler**
  * Implementation to supply reconnect delay
  * Default: Implementation that relies on other reconnect delay options

**Reconnect on Connect**
  * Whether to do reconnect logic if the initial connect fails
  * Default: true

**Reconnect forever on Connect**  
  * Whether max reconnect applies during Reconnect on Connect
  * Default: false

**Ignore Discovered Servers**
  * Only use bootstrap servers for reconnect purposes.
  * Default: false

**No Randomize**
* Whether to _not_ randomize server lists before connect and reconnect
* Default: false

**No Resolve Hostnames** 
  * Whether to _not_ resolve hostnames. 
  * Default: Resolve hostnames to ip addresses, which there may be multiple. 

## Other behavior

* Discovered servers combined with bootstrap make up the server list. Remove duplicates
* On reconnect, remove current server (that has been disconnected) from list and then place at end of list after randomize.

## Optional

* Provide a way for the user to provide the list of servers on connect or reconnect, instead of using the built in logic implementation.