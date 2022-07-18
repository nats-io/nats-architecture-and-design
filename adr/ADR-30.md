# Subject Transform

| Metadata | Value                   |
|----------|-------------------------|
| Date     | 2022-07-17              |
| Author   | @derekcollison, @tbeets |
| Status   | `Implemented`           |
| Tags     | server                  |

## Context and Problem Statement

As part of multiple technical implementations of the NATS Server, there is a need to create a mapping formula, or _transform_, that
can be applied to an input subject, yielding a desired output subject.

These implementations include:

* Subject mapping and partitioning
* JetStream RePublish
* Cross-account service and stream mapping
* Shadow subscriptions (e.g. across a leaf)

A subject transform shall be defined by a _Source_ filter that defines what input subjects are eligible (via match) to be
transformed and a _Destination_ subject format that defines the notional subject filter that the _transformed_ output 
subject will match.

Input subject tokens that match Source wildcard(s) "survive" the transformation and are reflected in the output
subject literally (or in some cases as a token-specific sub-transformation, e.g. subject partitioning).

## Design

Destination, taken together with Source, form a valid subject token transform. The resulting transform 
is applied to an input subject (that matches Source subject filter) to determine the the output subject.

### Transform rules

A given input subject must match the Source filter (in the usual subscription-interest way) for the transform to be valid. 

For a valid input subject:

* Source-matching literal-token positions are ignored, i.e. do not appear in the output subject
* Source-matching wildcard-token positions (if any) are _placed_ in the output subject in positions defined by the Destination format
* Source-matching `*` wildcard (single token) tokens must be placed in Destination format by wildcard cardinal position number using `$x` or `{{wildcard(x)}}` notation
* Source-matching `>` wildcard (multi token) tokens are mapped to the respective `>` position in the Destination format
* Literal tokens in the Destination format are mapped to the output subject unchanged (position and value)

### Example transforms

| Input Subject           | Source filter        | Destination format                   | Output Subject                    |
|:------------------------|----------------------|--------------------------------------|-----------------------------------|
| `one.two.three`           | `>`                   | `uno.>`                              | `uno.one.two.three`                 |
| `four.five.six`           | `>`                   | `eins.>`                             | `eins.four.five.six`                | 
| `one.two.three`           | `>`                   | `>`                                  | `one.two.three`                     | 
| `four.five.six`           | `>`                   | `eins.zwei.drei.vier.>`              | `eins.zwei.drei.vier.four.five.six` | 
| `one.two.three`           | `one.>`                | `uno.>`                               | `uno.two.three`                     |
| `one.two.three`           | `one.two.>`            | `uno.dos.>`                           | `uno.dos.three`                     |
| `one`                     | `one`                  | `uno`                                 | `uno`                               |
| `one.two.three.four.five` | `one.*.three.*.five` | `uno.$2.$1`                           | `uno.four.two`                      |
| `one.two.three.four.five` | `one.*.three.*.five`  | `uno.{{wildcard(2)}}.{{wildcard(1)}}` | `uno.four.two`                      |
| `one.two.three.four.five` | `*.two.three.>`        | `uno.$1.>`                             | `uno.one.four.five`                 |


