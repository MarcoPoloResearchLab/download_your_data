# ISSUES.md Format

This document describes the canonical ISSUES.md layout and the section-aware identifier scheme.

## Structure

- The file starts with a title line (for example, `# ISSUES`),
  followed by optional guidance text.
- Issues are grouped under level-2 headings (`## ...`).
- Optional subheadings (`### ...`) may be used within a section for organization (for example, "Recurring"), but IDs must still match the parent section. Recurring semantics are canonically represented by the identifier suffix; parsers normalize entries under a `Recurring` subheading to that suffix.
- Sections are:
  - BugFixes
  - Improvements
  - Maintenance
  - Features
  - Planning

## Issue Classification

Classify each issue by its requested outcome. Priority, urgency, affected code, and title words do not control the section.

Use this ordered test:

1. Use `BugFixes` only for an observed and reproducible violation of a current canonical contract.
2. Use `Features` for a new user or operator capability, public interface, resource kind, workflow, or product behavior.
3. Use `Improvements` for a one-time change to an existing capability, architecture, test system, or acceptance boundary.
4. Use `Maintenance` for repeatable upkeep under an unchanged solution contract. The same activity must remain valid for a future run.
5. Use `Planning` for analysis, a decision, or a plan that does not authorize implementation.

File each reproducible defect from an acceptance or migration issue as a separate BugFix issue. Split mixed outcomes across their correct sections.

Use priority and blocked state as separate attributes. Correct a misclassified unresolved issue before implementation. Preserve completed issue IDs as historical references.

## Resolved Issue Hygiene

Before archival, review each resolved non-recurring issue for durable product,
architecture, operator, security, testing, and skill consequences. Update each
affected source-of-truth document or skill before you move the issue.

Preserve the complete resolved entry and its identifier in the repository
archive. Keep unresolved, blocked, planning, and recurring issues in the active
tracker. Validate identifiers, dependencies, and duplicate IDs across both
files.
Section headings should not include numeric ranges; the section name alone is
the category.

## Issue entries

Each issue entry is a single list item with this shape:

```text
- [ ] [B042] (P1) {I007} Short title
```

Rules:

- `[ ]` means open (unresolved), `[-]` means taken (actively being worked, but still unresolved), `[!]` means blocked (unresolved), `[x]` means closed (resolved).
- The external ID is necessary.
- Priority and dependencies are optional and appear immediately after the ID.
- The title is necessary.
- Blocked issues (`[!]`) MUST include a short explanation in the body (at minimum one indented line starting with `Blocked:`).
- Write each new or changed title in ASD-STE100 Simplified Technical English.
## Identifiers

Format: `<SectionLetter><SequenceNumber>[R]` with no repo prefix.

Section letters:

- B = BugFixes
- I = Improvements
- M = Maintenance
- F = Features
- P = Planning

Identifiers must match the section they appear in. Numbers increment
independently per section. Use three digits (`001`-`999`) per section; after a
section reaches its max (example: after B999), the next auto-number wraps to
B001.
A capital `R` suffix inside the identifier marks the entry as recurring
(example: `[M001R]`). A separate `R` token after the identifier is invalid.
Parsers accept lowercase `r` while reading and render uppercase `R` in
canonical output.
Recurring entries represent standing or repeated work that should remain
visible during cleanup. Scheduling, timers, and job IDs are outside the
ISSUES.md format.
Legacy repo-prefixed identifiers (for example `IM-###`) are invalid.

## Priority and dependencies

- Priority uses `(P0)` through `(P2)` immediately after the ID.
- Dependencies use `{ID,ID}` with comma-separated IDs.

## Body text

- To attach a body on the same line, separate the title and body with a space,
  an em dash (U+2014), and a space.
- Additional body lines may follow on subsequent lines; indent by two spaces
  to keep them attached to the issue.
- Fenced code blocks are allowed in the body; indent them by two spaces as well.
- Structured issue bodies should use plain labels rather than Markdown
  headings. The canonical labels are `Goal:`, `Requirements:`,
  `Deliverables:`, `Validation:`, and `Blocked:`.
- `Goal:`, `Requirements:`, `Deliverables:`, and `Validation:` are recommended
  guidance for human and AI producers. Parsers recognize them but do not require
  every free-form issue body to contain all four labels.
- `Blocked:` is required only for blocked issues (`[!]`) and must include the
  concrete external dependency, missing input, or policy decision preventing
  progress.

## Example

```text
# ISSUES

## BugFixes
- [!] [B042] (P0) Fix crash on startup
  Goal:
  Prevent startup crashes during repository initialization.

  Requirements:
  Preserve the existing configuration loading contract.

Indent additional body lines by two spaces. Structured issue bodies must use plain labels:
  Deliverables:
  Patch the initialization path and document the failure mode.
  Validation:
  Reproduce the startup path with the affected configuration.

`Blocked:` is necessary only for blocked issues. It must identify the dependency, input, or policy decision that prevents progress.

  Blocked: waiting on upstream API credentials.
  ```bash
  timeout -k 30s -s SIGKILL 30s make test
  ```

Write each new or changed body in ASD-STE100. Use `.mprlab/AGENTS.DOCS.md` and `.mprlab/TERMINOLOGY.md`.
```