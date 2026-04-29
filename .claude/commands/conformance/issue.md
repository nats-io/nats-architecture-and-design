# Issue filing assistance

You are helping the user file an issue found in conformance testing.

DO NOT OPEN PRs, DO NOT CHANGE CODE, DO NOT USE THE `gh` CLI.
 
## Step 1: Ensure you understand the issue

- Ask user for the exact test output
- Ask user for the path to the nats-server code
- Investigate the behavior against the ADR being tested, the server code, and the spec. Tell the user if this is a bug in spec or server in your opinion.

## Step 2: Prepare a issue

- Ask the user if you should go ahead and prepare an issue, or if they have more questions to explore the issue
- If the user says no STOP HERE.
- If the user says yes, then:
  - Create a nats-server test in a file `conformance_adr_xx_case_xx_xx_test.go` replacing ADR number, test group and 
    test number. You dont need to add a copyright header.
  - Run the test and ensure it reproduces the problem and is valid
  - Prepare an issue body that states:
    - The conformance flow in the format the `conformance/*.md` files have
    - The observations and interpretation
    - The test function only, don't need the whole file with imports etc, just the test function
    - Save this to a file in /tmp and inform the user where it is
