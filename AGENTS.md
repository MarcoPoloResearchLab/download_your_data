# AGENTS.md

## Forward-Only Contract Discipline

This repository follows a forward-only, confident programming paradigm. This is a binding agent contract: no fallbacks, no backward compatibility, no legacy support, and no compatibility shims. Do not spend design or implementation effort on backward compatibility considerations except for explicit one-off data migrations into the current canonical contract.

Repeat for emphasis because this rule is binding: no fallbacks, no backward compatibility, no legacy compatibility. Delete or reject obsolete code paths, stale schemas, deprecated config, and old persisted shapes instead of preserving them through compatibility layers, dual reads/writes, aliases, or best-effort recovery.

One-off data migrations are allowed only when they move existing persisted data into the current schema in a bounded operation. After migration, remove the bridge and keep only the current contract.

<!-- BEGIN MPRLAB-GOVERNANCE -->
## MPR Lab Governance

Most workflow context files live under `.mprlab/`. The root `AGENTS.md` remains the repository entrypoint for agents.

Read these files before editing:

- `.mprlab/POLICY.md`: binding validation and confident-programming rules.
- `.mprlab/PLANNING.md`: durable planning contract.
- `.mprlab/issues-md-format.md`: issue tracker format and recurring identifier rules.
- `.mprlab/ISSUES.md`: active issue tracker.
- `.mprlab/AGENTS.GIT.md`: Git and pull request workflow.
- `.mprlab/AGENTS.GO.md`: Go guidance.
- `.mprlab/AGENTS.FRONTEND.md`: browser frontend guidance.

Do not create `.mprlab/AGENTS.md`. Scoped stack guidance belongs in `.mprlab/AGENTS.*.md` files.
If guidance conflicts, follow `.mprlab/POLICY.md` first, then root `AGENTS.md`, then the relevant scoped stack guide.
<!-- END MPRLAB-GOVERNANCE -->

<!-- BEGIN ISSUES.MD MANAGED ONBOARDING -->
## ISSUES.md repository workflow

ISSUES.md manages this repository through the current application contract.

- Use `.mprlab/ISSUES.md` as the repository issue tracker.
- Follow `.mprlab/issues-md-format.md` for issue syntax and identifiers.
- Use `.mprlab/runtime.yml` as the repository execution contract.
- Keep these required documents current through the ISSUES.md onboarding pull request.
<!-- END ISSUES.MD MANAGED ONBOARDING -->
