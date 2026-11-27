# Repository Policy Sections

## Selected Manifest Contract

The operator approved the v8 source cutover for this application.
This cutover preserves existing provider data and the permanent state-v1 contract.

- Keep the selected application manifest versionless.
- Keep `owner`, `release`, and `lifecycle` as the required baseline fields.
- Use the fixed `artifacts`, `publications`, and `targets` catalogs.
- Use explicit identity lists and ordered command lists.
- Declare CI and execution policy in the selected manifest's `lifecycle` field.
- Reject obsolete root collection shapes, unknown fields, and `schema_version`.

## Static Website Hosting

Apply this section to deployment or publication work for a browser frontend.

- Use GitHub Pages as the production host for each deployable browser frontend.
- Declare the browser frontend with a `targets.github_pages.sites[]` declaration in `.mprlab/deploy/resources.yml`.
- Use `gh-pages` as the publication branch.
- The GitHub Pages repository can differ from the application repository.
- Keep API and service routes on hostnames that differ from the website hostname.
- Reserve the GitHub Pages domain and its `www` hostname for GitHub Pages.
- Treat a container as an artifact source only when its static output goes to GitHub Pages.
- Verify publication through the public website and `/.mprlab-release.json`.
- Run the Governor check after each selected manifest change and before each release, publish, or deploy operation.

## Repository Guide Requirements

These guides retain repository-specific product or operational contracts.
Preserve those contracts during shared-governance updates. Apply current agent procedures from the core policy.

- Read and retain `.mprlab/AGENTS.DOCKER.md` for its task-specific scope.

## Repository CI Requirements

Implementation and shared-contract changes require final full CI. Use equivalent passing pull request CI for the same state or run the repository CI target locally.
For read-only audits, use source inspection. For documentation-only changes, run the applicable document and repository checks. For changes confined to `.mprlab/`, run the Governor consistency check and `git diff --check`, and review changed prose.
