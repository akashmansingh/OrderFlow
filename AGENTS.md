# OrderFlow Repository Instructions

## Project goal

OrderFlow is a portfolio backend for high-contention ticket reservations. The first release demonstrates correct inventory handling, an asynchronous reservation workflow, and reproducible operational evidence. PostgreSQL is authoritative for inventory and reservation state.

## Source of truth and architecture

- Read `docs/architecture.md` before changing service boundaries, persistence, event flow, or deployment.
- Keep the first release focused on the Reservation API and one background worker. Do not add unrelated products or infrastructure before the end-to-end reservation workflow works.
- Use PostgreSQL transactions and row-level locking to prevent overselling. Redis may accelerate short-lived holds and rate limiting, but must not be the authority for confirmed inventory.
- Write reservation state and its outbox event in the same database transaction. Assume at-least-once event delivery and make consumers idempotent; never claim exactly-once processing.
- Use Redpanda as the initial Kafka-compatible broker. Keep broker access behind small internal interfaces where useful; do not build a custom broker.
- Simulate payment. Do not handle real payment credentials or money in this portfolio project.
- Record architectural choices and material trade-offs in concise ADRs under `docs/decisions/`.

## Working rules

- Make routine, in-scope repository changes autonomously. Preserve unrelated user changes and inspect the diff before committing.
- Prefer small, coherent changes with clear interfaces and errors. Keep configuration explicit and never commit secrets, local `.env` files, credentials, or generated runtime data.
- Write or update tests for behavior changes. Test concurrency, duplicate events, worker restarts, expiry, compensation, and dependency failure where relevant.
- Do not claim a feature works, a test passes, or a performance target is met unless you ran the relevant command and observed the result. Keep measured load-test results with the exact workload and environment.
- Treat external Compose projects and inputs as untrusted. Never modify their tracked files to force a result, and never run broad Docker cleanup. Clean up only resources created by this project and report what was removed.
- Keep docs, API contracts, migrations, and implementation aligned. Review generated or staged diffs for secrets and unrelated files.

## Git, branches, commits, and pull requests

- Keep `main` releasable. Do feature work on a named branch such as `feat/reservation-outbox` or `fix/inventory-locking`; do not commit feature work directly to `main`.
- Before the first commit on a branch, tell the user which branch is active and what change it contains. If the current branch is not `main`, say so before committing. Keep the user informed when switching branches or when a commit will land on a different branch than expected.
- Make logical commits: one concern per commit, with a concise imperative subject that explains the outcome (for example, `Add transactional reservation outbox`). Do not create noisy, unrelated, or failing commits. Never amend, rebase, or rewrite shared history without explicit direction.
- After a coherent feature is implemented and verified, summarize the branch, commits, checks, and remaining risks, then recommend opening a pull request to merge that branch into `main`. Do not claim the PR is merged until it is. Creating or pushing a remote PR requires the user's request or established repository workflow authorization.
- Before proposing a merge, ensure the branch is based on current `main`, required checks pass, and the diff contains only the intended changes. Resolve conflicts deliberately; do not discard user work.

## Verification and completion

- Run focused tests and static checks for changed components; run the full suite when practical and report any commands not run.
- For reservation correctness, verify the confirmed quantity never exceeds available inventory under concurrent requests.
- A workflow milestone is complete only when its success and failure paths are demonstrated, including recovery after duplicate delivery or worker restart where applicable.
- Before describing work as complete, inspect `git diff` and `git status` and report the concrete evidence.
