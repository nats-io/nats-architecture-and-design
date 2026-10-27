# Add Strict Flag to JetStream Contexts

| Metadata | Value                                   |
|----------|---------------------------------------- |
| Date     | 2024-09-17                              |
| Author   | @caspervonb                             |
| Status   | `Draft`                                 |
| Tags     | jetstream, client                       |

## Context and Problem Statement

Client's with JetStream contexts currently lack a mechanism to enforce strict checking of response fields.
This leads to several issues:

1. Undetected misalignments or typos in field names, especially for rarely used features.
2. Silent acceptance of unknown fields in responses, potentially masking API changes or inconsistencies.
3. Difficulty in identifying discrepancies between client implementations and server responses.
4. Delayed discovery of issues, often only surfacing in production environments.
5. Challenges in maintaining consistent feature support across different client versions.

We're partially addressing this with a strict mode on the server side, but we can also add a strict flag to JetStream contexts to enforce stricter checking of responses.
This will help catch inconsistencies early, especially in test environments (e.g CI) and during feature development.

## Design

Add a `strict` boolean flag to JetStream context creation:

1. New `strict` parameter, defaulting to `false`.
2. When `true`, raise errors for responses with unknown fields.
3. Update parsing logic and implement error handling.

Example:
```python
js = jetstream.new(strict=True)
```

## Decision

Implement the opt-in strict flag as described. This feature will be valuable for catching inconsistencies early, especially in test environments and during feature development. The opt-in nature maintains backwards compatibility while providing a new tool for improving code quality
Documentation has to clearly state that this is a dangerous field and should only be set if it is really the wanted behavior.

## Consequences

Implementing the strict flag will enable early detection of misalignments and typos, potentially significantly improving robustness in test environments, especially for clients written in dynamic languages.
It will also provide better feedback on unimplemented features, aiding in maintaining consistency across client versions. However, this change introduces a slight increase in complexity and may lead to potential false positives if enabled in inappropriate environments (e.g production where the client is lagging behind the server but the features are not being used).
To mitigate these issues, clear documentation on usage will be provided, recommending its use primarily in test environments, we might even want to make it an undocumented feature of the clients.
