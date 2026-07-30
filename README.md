# Download Your Data

Download Your Data is one web product with two access boundaries:

- Anyone can browse the provider catalog, export guides, and Credits.
- A shared mpr-ui/TAuth account is required for provider data import,
  analysis, export, replacement, and deletion.

Netflix, OpenAI, and future analysis apps use one TAuth tenant and one
application user identity. Authentication to Netflix, OpenAI, or another data
provider remains entirely on that provider's own site.

## Current routes

| Surface | Route | Authentication | Protected API |
| --- | --- | --- | --- |
| Provider catalog | `#catalog` | No | No |
| Provider guide | `#guide/{provider}` | No | No |
| Credits | `#credits` | No | No |
| Provider application | `#app/{provider}` | Shared TAuth session | Yes |

There is no `#provider/{provider}` compatibility route and no end-user
operator CLI. The executable accepts only `serve`.

## Authentication boundary

The browser loads the literal `mpr-ui@latest` bootstrap and uses app-owned
`/config-ui.yaml` as its only authentication input. Application code reacts to
the documented `mpr-ui:auth:authenticated` and
`mpr-ui:auth:unauthenticated` events. It does not load `tauth.js`, call TAuth
endpoints, inspect cookies or tokens, or infer login state from an API
response.

The Go API validates the TAuth session with
`github.com/tyemirov/tauth/pkg/sessionvalidator`. Every protected request
receives one immutable user containing the exact tenant and user IDs. Every
provider path is resolved below:

```text
<private-data-root>/users/<opaque-tenant-user-digest>/<provider>
```

An opaque resource identifier never grants cross-user access. An authenticated
request for another user's resource receives the same not-found contract as an
unknown resource. `DELETE /api/workspace` with the exact confirmation
`delete-download-your-data-workspace` removes all application-owned provider
data for the current user.

## Runtime configuration

The server fails closed unless all required authentication and ownership
values are present:

```text
DOWNLOAD_YOUR_DATA_DATA_DIR
DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN
DOWNLOAD_YOUR_DATA_API_ORIGIN
DOWNLOAD_YOUR_DATA_TAUTH_URL
DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID
DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY
DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME
DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME
DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID
```

The TAuth URL, tenant, signing key, cookie names, and Google client ID must
match the selected TAuth environment exactly. Hosted origins require HTTPS;
HTTP is accepted only for loopback development. The private data root is
mandatory and must satisfy the owner-only filesystem contract.

`DOWNLOAD_YOUR_DATA_ADDRESS` controls the listen address and defaults to
`127.0.0.1:8787`; the selected production profile must set its exact container
bind address. Optional server-owned integration values include:

```text
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN
DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL
DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY
```

Start and stop the configured server with:

```bash
make up
make down
```

`make up` remains attached to its terminal. `make down` is idempotent and
refuses to stop a process that is not the exact server recorded by this
checkout.

## API boundary

`GET /api/health` is public and contains no user data. `/config-ui.yaml` and
static assets are public. Every other `/api/*` route requires a valid TAuth
session.

The protected API permits only the exact configured frontend origin, uses
credentialed CORS, and requires the per-process CSRF token for mutations.
Frontend requests use `credentials: include`. A `401` after mpr-ui has reported
authenticated is displayed as an integration failure; it never starts an
application-owned login flow.

Netflix supports user-owned CSV import, analytics, records, progress,
replacement, optional TMDB enrichment, export, provider deletion, and complete
workspace deletion. OpenAI snapshots and search are user-scoped; browser
archive import and indexing remain unavailable until their authenticated job
lifecycle is implemented.

## Validate

```bash
make ci
```

The full gate runs formatting, static analysis, checked JavaScript, Go
contracts, matcher evaluation, local lifecycle checks, asset validation, and
real-browser public/authenticated workflows.

## Release and deployment

The obsolete macOS application archive and static download landing page have
been removed. The canonical lifecycle entrypoints remain:

```bash
make release
make publish
make deploy-dry-run
make deploy
```

They currently fail closed because the exact split-origin production profile
does not yet exist. No API hostname, TAuth tenant, cookie contract, OAuth
callback, gateway port, storage mount, inference service, or secret reference
is guessed. See [.mprlab/deploy/README.md](.mprlab/deploy/README.md) and
[the authentication plan](docs/user-authentication-plan.md) for the required
profile and target topology. Only the user runs `make deploy` after those
contracts and their non-mutating validation are complete.
