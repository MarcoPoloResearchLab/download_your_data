# AGENTS.md

## Forward-Only Contract Discipline

This repository follows a forward-only, confident programming paradigm. This is a binding agent contract: no fallbacks, no backward compatibility, no legacy support, and no compatibility shims. Do not spend design or implementation effort on backward compatibility considerations except for explicit one-off data migrations into the current canonical contract.

Repeat for emphasis because this rule is binding: no fallbacks, no backward compatibility, no legacy compatibility. Delete or reject obsolete code paths, stale schemas, deprecated config, and old persisted shapes instead of preserving them through compatibility layers, dual reads/writes, aliases, or best-effort recovery.

One-off data migrations are allowed only when they move existing persisted data into the current schema in a bounded operation. After migration, remove the bridge and keep only the current contract.

<!-- BEGIN MPRLAB-GOVERNANCE -->
## MPR Lab Governance

Root `AGENTS.md` is the agent entrypoint. Shared rules live under `.mprlab/`.

Read `.mprlab/POLICY.md` for every task.
Read the following files only when their condition applies.
Open each selected guide and read its full contents before acting. If the output is truncated, read the remaining chunks.
Follow required references at every depth, including Markdown links and named files in prose or tables. Apply the conditions in each document. A scoped reference selects the named section or record.
Resolve relative links from the document that contains them and explicit repository paths from the repository root. Reuse content already read in this context, including when a link leads back to it.
Before the dependent action, apply the requirements you found. If a required reference is unavailable, identify it and the affected action, and continue independent work.

- Before edits: `.mprlab/PLANNING.md`.
- Before editing repository technical prose: `.mprlab/AGENTS.DOCS.md` and `.mprlab/TERMINOLOGY.md`.
- For explicitly required ASD-STE100 compliance: `.mprlab/AGENTS.STE.md`.
- For application onboarding or user-interface behavior: `.mprlab/AGENTS.PRODUCT.md`.
- For deployment manifests, release, publication, deployment, or native preparation: `.mprlab/AGENTS.DEPLOYMENT.md`.
- For issue work: the selected issue and its dependencies in `.mprlab/ISSUES.md`.
- For tracker edits: `.mprlab/issues-md-format.md`.
- For Git operations: `.mprlab/AGENTS.GIT.md`.
- For HTTP or gRPC API changes: `.mprlab/AGENTS.API.md`.
- For Go changes: `.mprlab/AGENTS.GO.md`.
- For Python changes: `.mprlab/AGENTS.PY.md`.
- For browser changes: `.mprlab/AGENTS.FRONTEND.md`.
- For container changes: `.mprlab/AGENTS.DOCKER.md`.

File permission modes are outside agent scope.
Never examine, validate, compare, require, change, or record a file permission mode.
Never use a file permission mode in acceptance, security, credential, execution, publication, deployment, or failure analysis.
The values `0600` and `7777` have no governance meaning.
This rule does not change service authorization or operation authority.

Always reference each issue by its ID, for example `B001` or `I027`.
Never use an `ISSUES.md` file path, line number, or `path:line` syntax as an issue reference.

Do not create `.mprlab/AGENTS.md`. Scoped guidance belongs in `.mprlab/AGENTS.*.md` files.
If guidance conflicts, obey `.mprlab/POLICY.md` first, then root `AGENTS.md`, then the applicable scoped guide.
<!-- END MPRLAB-GOVERNANCE -->

<!-- BEGIN ISSUES.MD MANAGED ONBOARDING -->
## ISSUES.md repository workflow

ISSUES.md manages this repository through the current application contract.

- Use `.mprlab/ISSUES.md` as the repository issue tracker.
- Follow `.mprlab/issues-md-format.md` for issue syntax and identifiers.
- Use `.mprlab/runtime.yml` as the repository execution contract.
- Keep these required documents current through the ISSUES.md onboarding pull request.
<!-- END ISSUES.MD MANAGED ONBOARDING -->
