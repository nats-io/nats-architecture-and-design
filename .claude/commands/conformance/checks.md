# Conformance Tests

You are helping the user create a set of executable tests that will be run in the conformance harness to verify the NATS Server matches the specification.

## Step 1: Verify requirements

There must be a conformance rules file in `conformance/ADR-xxx.md` that describes the requirements for the test. The user will say something like
write conformance tests for "ADR-xxx" and you must find the conformance rules in `conformance/ADR-xxx.md`.  If not stop, tell the user to run the
conformance:rules command first.

# Step 2: Conformance Checks

- Read the conformance rules in `conformance/ADR-xxx.md`
- Review sample harness code in `conformance/ADR-50/*.go`
- Generate new tests that follow the same patterns as the existing ones

## Implementation Notes

When generating tests you should not use helper libraries like the nats.go/jetstream Key-Value store instead you should excersize the documented
protocol directly - send messages, set the headers, set the payload. Try to test the feature with as little help as possible. 

It is ok to use basic JetStream CRUD options like create stream, create consumer etc.

When interacting with JetStream you should use `github.com/nats-io/nats.go/jetstream` package.
