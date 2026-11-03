# gmVector backlog

Reported issues are not confirmed defects until reproduced against the relevant
source and API contract. Recording an issue does not authorize expanding an
active work package.

## BUG-001 — Employee name missing in project API data

- Reported: 2026-11-03, by the user.
- Status: reported; reproduction and contract check pending.
- Priority: normal, pending triage.
- Report: the API does not show the name of an employee added to a project.
- Expected outcome: the employee can be identified by name in the relevant
  project/integration flow.
- Investigation: identify the affected endpoint and response, reproduce with
  synthetic data, and compare with OpenAPI. Determine whether a name is omitted
  from its intended response or whether the consumer fails to resolve an employee
  ID. Define a compatible fix before changing the contract.
- Scope: future work; no implementation or verification claimed. Kept separate
  from WP-05 operational acceptance.
