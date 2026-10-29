Development Workflow — Document First

This repository follows a Document-First, Evidence-Driven Development approach.

1. Documentation is the source of truth. Before implementation, review the relevant architecture, requirements, decisions, and existing code. Never assume documentation or previous agent reports are correct without verification.
2. Plan before coding. Define scope, constraints, affected components, acceptance criteria, and verification steps. Keep changes minimal and focused.
3. Resolve documentation conflicts first. If implementation requires changing an architectural decision or established contract, update and approve the authoritative documentation before modifying the code.
4. Implement within scope. Do not introduce unrelated refactoring, new dependencies, architectural changes, or undocumented behavior without explicit approval.
5. Verify with evidence. Run relevant tests and checks. Report actual results, limitations, and remaining risks. Never claim tests passed unless they were executed.
6. Independent review. Significant changes require a separate, read-only review against the documentation, implementation, and acceptance criteria. The review must not rely solely on the implementing agent's claims.
7. Human authority. Agents may propose solutions, but unresolved architectural decisions, scope expansions, and exceptions require explicit human approval.

Core principle: Documentation → Plan → Approval (when required) → Implementation → Verification → Independent Review → Acceptance.

Apply rigor proportionally to risk and complexity. Small, low-risk changes should not require unnecessary process overhead.
