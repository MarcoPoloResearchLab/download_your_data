# Deployment contract

This repository owns two deliberately separate surfaces:

1. The product is one macOS arm64 executable. It serves its embedded workspace
   and API on loopback and keeps personal data in the local private data root.
2. The public deployment is a static GitHub Pages download and documentation
   page. It never serves the product API and receives no personal data.

## Lifecycle

The only release-operator sequence is:

```bash
make release
make publish
make deploy
```

`make release` runs the complete CI gate, builds the executable twice to prove
determinism, packages the exact macOS arm64 bundle and Pages site, creates the
checksum, prepares one CHANGELOG-only release commit and annotated tag, and
seals every payload beneath `.git/mprlab-release`.

`make publish` pushes the prepared default-branch commit and tag, creates the
non-draft GitHub Release, uploads the sealed assets without rebuilding, and
downloads them again to verify their hashes.

`make deploy` downloads the published manifest and Pages payload, verifies them
against the prepared manifest and remote tag, replaces `gh-pages`, configures
branch-based GitHub Pages, and verifies the public source marker.

`make deploy-dry-run` is non-publishing validation for repository and gateway
work. It never pushes a branch, creates a release, configures Pages, or changes
production.

## Production profile

- Frontend origin:
  `https://marcopoloresearchlab.github.io/download_your_data/`
- Frontend owner: GitHub Pages branch publishing from `gh-pages:/`
- DNS owner: GitHub's default organization Pages domain; there is no custom
  application hostname or `CNAME`
- Backend/API origin: none
- Origin topology: one static public documentation surface; the product API is
  not deployed
- Reverse proxy and upstream: none
- Container, service port, or gateway host: none
- OAuth callback: none
- Authentication, tenant, session cookies, CORS credentials, and TAuth:
  not applicable
- Browser runtime secrets or deployment configuration: none

The released application itself remains same-origin on its selected loopback
address, normally `http://127.0.0.1:8787`. It does not load `mpr-ui`, call
TAuth, expose a public API, or accept a non-loopback listen address.

## Gateway ownership

`.mprlab/deploy/resources.yml` is the canonical gateway discovery manifest.
The gateway may validate and dispatch the declared Pages target, but it does
not own or copy this repository's release, publication, or deployment
implementation.
