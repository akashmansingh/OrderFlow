# Workspace Working Rules

## Autonomous repository work

- Proceed with routine, reversible work that is within the requested repository task.
- Preserve unrelated user changes and inspect repository status and diffs before editing or committing.
- Ask before destructive operations, publishing, pushing, changing external systems, or handling secrets beyond the project's established workflow.

## Logical commits

- Commit each coherent, verified change as a small logical unit.
- Keep one concern per commit and use an imperative message that explains the delivered outcome.
- Do not commit failing or unverified work unless it is explicitly identified as a temporary checkpoint.
- Before committing, tell the user the active branch and state clearly when commits are being made on a branch other than `main`.
- After the feature is verified, report commits and checks and recommend creating a PR from the feature branch into `main`. Never imply a PR exists or is merged until that is true.
