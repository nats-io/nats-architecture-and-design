# Using and Extending

This is a directory that holds entirely Claude generated tests and rules.

The intention is that Claude acts as a 3rd party reader of the specifications and test these specifications using basic
NATS primitives - publish/subscribe/request with crafted bodies and headers.

The purpose is to create consensus between ADR writer, Server implementer and Claude as a 3rd party reader. If we all agree
then the outcome is better, the ADR is clearer and the implementation is more correct.

## Adding coverage

We provide 3 Claude commands; the basic flow is as follows:

```ignorelang
> /conformance:rules ADR-xx
```

This will read the ADR in question and generate a set of rules in `conformance/ADR-XX.md`, before doing it Claude will
potentially ask you to clarify areas of ambiguity, clarify some behaviors and agree on what it will find hard to test from
the outside of the server and so will be out of scope.

During this stage you may ask it to update the ADR with some clarifications and adjust its generated rules accordingly.

This is where you get on the same page, define scope and clarify ambiguities.

Once you have the rules, generate the checks:

```ignorelang
> /conformance:checks ADR-xx
```

This should mainly be self-driving and write the tests matching the rules.

When you build and run this against a cluster, it will log issues like this:

```
│ FAIL   │ ADR-51-SCH │ SCH-504 │ Invalid Nats-Schedule-Time-Zone value rejected     │ expected zone="" (empty value) to be rejected, got success &{Stream:CONF_SCH_50… │  159ms │
```

A `FAIL` is a test that found issues.  `INCO` means the outcome is inconclusive, the check phase will write some tests as
exploration in order to determine the running behavior of a real server.  You should focus on each of these FAIL and INCO 
issue and try to investigate the root cause.  `INCO` ones will likely result in ADR or test updates once you agree on the 
correct behavior.

To investigate each issue use:

```ignorelang
> /conformance:issue server code in ../nats-server/server issue: <output from the test as above>
```

It will use the server code to dig in deep and find the root cause and guide you through a solution.  The outcome might 
be issue text, reproducing server unit test and more saved in `/tmp`.

If you find the issue is a ADR bug then ask it to just fix the ADR and open a PR (manually).