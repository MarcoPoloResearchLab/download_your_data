# Production Deployment

## Status

Download Your Data has one complete repository-owned schema-v3 production
contract. As of 2026-08-02, it has not been released, published, or deployed.
The existing DNS records point the intended frontend to GitHub Pages and the
API hostname to the MPR gateway host, but neither hostname is serving this
application yet.

The actual activation remains three distinct operator-owned lifecycle steps:

```bash
make release
make publish
make deploy
```

An agent can prepare and run non-mutating plans. Only the user runs
`make deploy`.

## Exact Profile

[`configs/production.yml`](../configs/production.yml) is the validated
non-secret source for the browser artifact and the parity checks against
[`resources.yml`](../.mprlab/deploy/resources.yml).

| Concern | Exact value |
| --- | --- |
| Public frontend | `https://dyd.mprlab.com` |
| Protected API | `https://dyd-api.mprlab.com` |
| Browser-facing TAuth | `https://dyd-api.mprlab.com` |
| TAuth tenant | `download-your-data` |
| Google web client ID | `283383931996-582q1pholigban5bueqfq4g470hlrfpf.apps.googleusercontent.com` |
| Login path | `/auth/google` |
| Logout path | `/auth/logout` |
| Nonce path | `/auth/nonce` |
| Session path | `/auth/session` |
| Session cookie | `download_your_data_session` |
| Refresh cookie | `download_your_data_refresh` |
| Cookie policy | Domain `.mprlab.com`, `Secure`, `SameSite=None` |
| Credentialed CORS origin | `https://dyd.mprlab.com` |
| API address | `0.0.0.0:8787` |
| Health path | `/api/health` |
| Persistent mount | `/var/lib/download-your-data` |
| Retained Docker volume | `mprlab-download-your-data-data` |
| Public image | `ghcr.io/marcopoloresearchlab/download-your-data` |

The Google web client ID is public browser configuration, not a secret. Its
Google Identity Services configuration must permit the exact Pages origin.
The shared TAuth flow posts to `/auth/google`; there is no separate application
callback route.

## Resource Graph

The selected application asks the sibling gateway to reconcile only these
typed resources:

1. Two app-private values: the Google web client ID and an independent TAuth
   JWT signing key.
2. One public Linux/AMD64 API image and one retained user-data volume, placed
   once on the `gateway` inventory group.
3. The same-host `download-your-data.http` runtime capability on port `8787`.
4. One automatic-TLS Caddy route for `dyd-api.mprlab.com`:
   `/auth` and `/me` use `tauth.http`; `/` uses
   `download-your-data.http`.
5. One public health check at
   `https://dyd-api.mprlab.com/api/health`.
6. One container-rendered GitHub Pages artifact for `dyd.mprlab.com`.
7. One `download-your-data` tenant contribution to the active
   `tauth.tenants` handler.

The app does not own Compose YAML, Ansible, host inventory, Caddy templates,
TAuth configuration files, or gateway controllers. Those are generated and
reconciled by `mprlab-gateway` only when this manifest requests them.

## Artifacts

The Dockerfile has two final targets:

- `pages` contains only the rendered static browser site. The render binds the
  frontend and API origins, writes the production Content Security Policy and
  `/config-ui.yaml`, materializes the resource library, sitemap, robots file,
  and browser assets, and rejects a non-empty output directory.
- `api` contains the CGO/SQLite Go executable, CA certificates, and the SQLite
  runtime library. It runs as `65532:65532` and writes application data only
  beneath the retained mount.

`make test-production-artifacts` builds both targets for Linux/AMD64, checks
the Pages files for unresolved markers and deployment inputs, starts the API
image with a read-only root filesystem, and verifies `/api/health` as the
non-root user. The standard Docker client is the artifact authority.

## Private Input

The sole private deployment input is `.mprlab/deploy/.env`. It must remain
untracked, ignored, mode `0600`, and excluded by `.dockerignore`. It contains
exactly:

```text
DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=<the public client ID above>
DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=<independent private signing key>
```

Release and publication do not read this file. Deployment reads it to generate
the API environment and the selected TAuth tenant candidate without writing
secret bytes into the resource manifest, generated Pages site, image, active
registry, or lifecycle receipt.

## Current Capability Boundary

The current production manifest deliberately declares no inference provider
and no TMDB secret. Therefore:

- public guides and the authenticated shell are deployable;
- raw Netflix import, analytics, records, and CSV export are deployable;
- optional Netflix TMDB enrichment is unavailable;
- OpenAI archive upload and indexing remain unavailable.

Do not infer an inference hostname or copy an operator secret into this app.
Add those capabilities only after their exact current contracts are frozen.

## Non-Mutating Verification

The gateway plan targets require clean committed sibling checkouts. From
`mprlab-gateway`, run:

```bash
make plan-app-release MPRLAB_APP_ROOT=/absolute/path/to/download_your_data
make plan-app-publish MPRLAB_APP_ROOT=/absolute/path/to/download_your_data
make plan-app-deploy MPRLAB_APP_ROOT=/absolute/path/to/download_your_data
```

The deploy plan reads the private input and real operator inventory but does
not connect to or mutate production. It must resolve one `gateway` placement
and seal the `tauth.http` and `tauth.tenants` references, every private output,
and the exact Pages/API output graph.

## Activation And Acceptance

After the source branch is reviewed and merged, the user runs the three
zero-argument lifecycle commands in order. A successful release alone does not
enable Pages. A successful publication alone does not activate Pages, Caddy,
TAuth, or the API container. Deployment owns that activation.

After deployment, verify all of these public boundaries before calling the
product live:

1. `https://dyd.mprlab.com/.mprlab-release.json` identifies the deployed
   published release.
2. `https://dyd.mprlab.com/config-ui.yaml` contains the exact profile above.
3. A fresh signed-out browser can read the catalog and provider guides without
   a protected API request.
4. `https://dyd-api.mprlab.com/api/health` returns the API health contract.
5. The browser CORS response permits only `https://dyd.mprlab.com` with
   credentials.
6. Google sign-in establishes the two cross-origin secure cookies through the
   shared shell, a reload restores the session, and sign-out clears it.
7. An authenticated user can import a Netflix viewing-history CSV, view raw
   analytics, export results, and delete the provider workspace.

Any failed source marker, TLS, CORS, cookie, TAuth, or authenticated-workspace
step blocks production acceptance.
