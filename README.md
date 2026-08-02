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
| Resource library | `/resources/` | No | No |
| Credits | `#credits` | No | No |
| Provider application | `#app/{provider}` | Shared TAuth session | Yes |

There is no `#provider/{provider}` compatibility route and no end-user
operator CLI. The executable accepts only `serve`.

The path-based resource library is the indexable companion to the interactive
provider guides. Every resource uses a trailing-slash canonical URL, links to
related provider workflows, and derives its canonical, Open Graph, JSON-LD,
sitemap, and robots URLs from `DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN`. The
repository does not hard-code the unresolved production host. See the
[SEO resource library contract](docs/seo-resource-library.md) for the page
inventory, evidence model, indexing rules, and publication boundary.

## Supported platforms

| Platform | Surface Type | Primary Input / Scope | Capabilities & Status |
| --- | --- | --- | --- |
| **Netflix** | Interactive Workspace & Guide | Viewing activity CSV (`Title`, `Date`) | **Live Workspace (`#app/netflix`) & Guide (`#guide/netflix`)** — Per-profile CSV import, raw activity analytics, date/weekday filtering, paged records, optional TMDB enrichment, enriched CSV export, and provider data deletion. |
| **OpenAI** | Interactive Workspace & Guide | ChatGPT Data Export ZIP (`conversations.json`) | **Live Guide (`#guide/openai`) & Workspace in Progress (`#app/openai`)** — Step-by-step export guide live; user-owned archive ingest, indexing, and hybrid semantic search engine contract in active development. |
| **Facebook** | Visual Export Guide | Meta Accounts Center Information Archive | **Live Guide (`#guide/facebook`)** — Product-specific visual export walkthrough, Accounts Center navigation, file format/media options, and first-party help links. |
| **Instagram** | Visual Export Guide | Meta Accounts Center Information Archive | **Live Guide (`#guide/instagram`)** — Product-specific visual export walkthrough, Accounts Center download steps, data type selection, and first-party help links. |
| **WhatsApp** | Visual Export Guide | Chat Export & Account Information Archive | **Live Guide (`#guide/whatsapp`)** — Product-specific visual export walkthrough for account info reports and individual chat history exports (`.txt`/media). |
| **Threads** | Visual Export Guide | Meta Accounts Center Export Archive | **Live Guide (`#guide/threads`)** — Product-specific visual export walkthrough explaining Threads export scope via Instagram Accounts Center. |
| **LinkedIn** | Visual Export Guide | LinkedIn Data Privacy Archive | **Live Guide (`#guide/linkedin`)** — Visual export guide for requesting account data archives (messages, connections, profile, activity) from Data Privacy settings. |
| **TikTok** | Visual Export Guide | TikTok Request Data Archive | **Live Guide (`#guide/tiktok`)** — Visual export guide for requesting and downloading personal account archives (profile, activity, comments, history) in TXT/JSON. |
| **X (Twitter)** | Visual Export Guide | Download Your Archive (ZIP) | **Live Guide (`#guide/x`)** — Visual export guide for requesting X account archive, password verification, and downloading personal ZIP data. |
| **YouTube** | Visual Export Guide | Google Takeout YouTube Archive | **Live Guide (`#guide/youtube`)** — Visual export guide for selecting YouTube & YouTube Music data, configuring delivery options, and downloading Takeout archives. |
| **Google** | Visual Export Guide | Google Takeout Multi-Service Archive | **Live Guide (`#guide/google`)** — Visual export guide for configuring Google Takeout multi-service exports, export frequencies, file sizes, and destination options. |
| **Amazon** | Visual Export Guide | Order History Reports & Personal Data Archive | **Live Guide (`#guide/amazon`)** — Product-specific visual export walkthrough for requesting Amazon order reports, Kindle content, and Prime Video history. |

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

`make up` is the zero-argument local entrypoint. It loads the checkout's exact
ignored `configs/.env` file, protects it with mode `0600`, starts the official
`TAuth:latest` service and same-origin `ghttp:latest` gateway through Docker,
starts the application, and reports success only after all three boundaries
are ready. The browser front door is `http://localhost:8080`; `/auth` and
`/me` route to TAuth while every other path routes to the application.
Inherited `DOWNLOAD_YOUR_DATA_*` values do not override the file. Tracked
example environments are not runtime inputs.

The local profile may contain the exact
`DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=GENERATE_ON_FIRST_MAKE_UP` bootstrap
marker once. On first startup, the lifecycle replaces that marker atomically
with a new private signing key before either process reads the profile. The
Google web client ID is browser-safe and is shared by the app and TAuth; a
Google OAuth client secret is not a runtime input for this GIS flow.

The local `configs/.env` must contain all required authentication and ownership
values:

```text
DOWNLOAD_YOUR_DATA_ADDRESS
DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM
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
mandatory, must use an absolute path, and must satisfy the owner-only filesystem
contract. Optional integration values may be present in the same file:

```text
DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN
DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL
DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY
```

The local public, API, and TAuth origins must be identical. The application
listen address and Docker-only application upstream must describe the same
backend. Start and stop the complete stack with:

```bash
make up
make down
```

`make up` remains attached to its terminal. `make down` is idempotent, requires
no environment file, stops only the application process owned by this checkout, and
removes its checkout-scoped TAuth and gateway containers. The named TAuth data
volume remains intact across normal stop/start cycles.

## Source layout

The repository root contains only project-wide entrypoints and governance.
Application ownership is explicit:

```text
cmd/download-your-data/  executable bootstrap
internal/httpapi/        authenticated HTTP and provider adapters
internal/                provider domains, storage, inference, and retrieval
frontend/                browser app, public resources, content, manifests, and images
scripts/                 repository-owned development and validation commands
testdata/                fixtures shared by multiple packages
```

Generated executables and browser-run state are not source. Remove them with:

```bash
make clean
```

## API boundary

`GET /api/health` is public and contains no user data. `/config-ui.yaml`,
`/resources/`, `sitemap.xml`, `robots.txt`, and static assets are public. Every
other `/api/*` route requires a valid TAuth session.

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
