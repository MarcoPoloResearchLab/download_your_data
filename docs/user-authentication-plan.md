# User Authentication And Provider Workspace Plan

## Decision

`download_your_data` will become one web product with two deliberately
different access boundaries:

1. A static public catalog and provider-guide surface that requires no
   Download Your Data account.
2. Authenticated provider applications for data import, analysis, export,
   replacement, and deletion.

The public frontend remains a repository-owned GitHub Pages artifact. All
provider applications use one Download Your Data TAuth tenant, one TAuth
session, one `mpr-ui` authentication lifecycle, and one user identity. Netflix,
OpenAI, and future providers must not introduce provider-specific Download Your
Data login flows.

This supersedes the local-only, unauthenticated product and packaged macOS
application as the current target. The implementation must replace that
contract rather than preserve local and hosted modes in parallel. Current
end-user operator commands are also retired; maintained analysis capabilities
move behind authenticated application routes and jobs instead of retaining an
unscoped local data workflow.

## Product Boundary

Users authenticate to Download Your Data only when they enter a data-analysis
application. Provider guides remain public, including the steps that send a
user to Netflix, OpenAI, or another provider to obtain an export.

Authentication to the external provider remains outside this product:

- Download Your Data never receives a Netflix, OpenAI, Google, Meta, or other
  provider password, verification code, OAuth token, or session.
- Guide links may lead to a provider's own signed-in surface.
- The user returns with an export and uploads it only to the authenticated
  Download Your Data application.

| Surface | Canonical route | Authentication | Application API |
| --- | --- | --- | --- |
| Provider catalog | `#catalog` | Not required | No protected request |
| Provider guide | `#guide/{provider}` | Not required | No protected request |
| Provider application | `#app/{provider}` | Shared TAuth session | Required after authentication |
| Credits and privacy | Static routes | Not required | No protected request |

The current `#provider/{provider}` application route is replaced by
`#app/{provider}`. No route alias or compatibility redirect is retained.

## Authentication Ownership

`mpr-ui` and TAuth are the only browser authentication authority.

- The static frontend owns `/config-ui.yaml`.
- The page loads `mpr-ui.css`, `mpr-ui-config.js`, and `mpr-ui.js` from the
  literal `mpr-ui@latest` contract.
- `<mpr-header data-config-url="/config-ui.yaml">` owns login presentation,
  session restoration, refresh, and logout through the documented shared
  lifecycle.
- Application code listens for `mpr-ui:auth:authenticated` and
  `mpr-ui:auth:unauthenticated`.
- Application code must not load `tauth.js`, call TAuth endpoints, inspect
  cookies, storage, tokens, or claims, or infer authentication from an
  application API response.
- A provider application makes its first protected request only after
  `mpr-ui` reports `authenticated`.
- On `unauthenticated`, the frontend cancels provider work, closes event
  streams, revokes object URLs, and clears all app-owned workspace state.
- A protected API failure after `mpr-ui` reports authenticated is rendered as
  an authorization, workspace, or deployment-integration failure. It never
  starts another login flow.

The authenticated shell uses the shared `auth-transition` surface. Its
completion event fires only after the requested provider application has
hydrated. A synchronous page bootstrap buffers only the documented
`mpr-ui:auth:authenticated` and `mpr-ui:auth:unauthenticated` events until the
application module attaches its listeners. Completion waits for
`MPRUI.whenAutoOrchestrationReady()`.

The current literal `mpr-ui@latest` bundle does not expose a documented
authentication-state snapshot helper and can settle a cold signed-out session
without emitting an initial unauthenticated event. The application therefore
keeps a requested provider application locked in a pending state until a
documented lifecycle event arrives. Public guides remain fully available. Cold
session restoration cannot be called complete until `mpr-ui` supplies a public
settlement signal; application code must not work around the gap by inspecting
component internals, cookies, storage, tokens, claims, or TAuth endpoints.

Public guides render independently of authentication settlement. A fresh or
signed-out browser can read every guide, follow every first-party instruction
link, change locale and theme, and return to the catalog without a protected
application request.

## User Identity

The backend validates the TAuth-issued session with the current published
`github.com/tyemirov/tauth/pkg/sessionvalidator` package. The exact signing key,
issuer, tenant, and cookie name come from the selected deployment profile.

At the HTTP boundary, validated TAuth claims become one immutable
`AuthenticatedUser` value containing:

- the exact TAuth tenant ID;
- the exact TAuth user ID.

Email, display name, avatar, roles, and raw claims are not storage keys and are
not required by provider-domain code. The browser may display profile data
through `mpr-ui`; the application backend does not create a second profile or
identity-provider mapping.

Every protected service and repository method accepts the authenticated user
value explicitly. A global or optional current user is forbidden.

## User-Owned Data

Every application resource is owned by the pair `(user, provider)`:

- provider snapshot;
- generation and active-generation pointer;
- source upload and staging data;
- analytics and record artifacts;
- progress events and leases;
- TMDB cache entries and accepted metadata;
- OpenAI databases, indexes, vectors, reports, and query cache;
- streamed exports and deletion state.

Opaque generation IDs never grant access by themselves. Repositories resolve
them beneath the authenticated user's provider root. A resource belonging to a
different user is not visible, mutable, streamable, or downloadable.

The production service uses one configured private storage root with
user-scoped subdirectories and owner-only filesystem permissions. Directory
names use a stable non-secret digest of tenant ID and TAuth user ID rather than
email or display name. Logs contain only opaque user, provider, generation,
operation, count, duration, and typed error identities.

Upload, working-disk, active-job, generation-count, and retained-data limits
are enforced per user and per provider. One user's long-running job or quota
failure must not block an unrelated user's workspace.

There is no automatic read of the current unscoped local data root. Users
re-import source exports into their authenticated workspace. If a bounded
one-off migration is later required, it must be planned explicitly, run once,
and remove its bridge immediately.

TAuth account deletion and Download Your Data workspace deletion remain
different ownership boundaries. TAuth owns the account. This application owns
an explicit authenticated full-workspace deletion operation that removes every
provider artifact for the current user.

## HTTP And Browser Contract

Public surfaces:

- static HTML, JavaScript, CSS, icons, screenshots, localization, and
  `/config-ui.yaml`;
- no backend secret or deployment-only runtime value;
- no personal-data upload form outside an authenticated provider route.

Browser-facing authentication surfaces:

- the exact TAuth origin and paths declared by `/config-ui.yaml`;
- `/me` and refresh/logout behavior owned by `mpr-ui` and TAuth;
- no application-owned credential exchange.

Application API:

- health and deployment probes may be public and contain no user data;
- provider capabilities, snapshots, uploads, events, analytics, records,
  exports, replacement, and deletion are protected;
- an absent or invalid session returns `401`;
- an authenticated request for a resource outside the user's workspace returns
  the same not-found contract as an unknown opaque resource;
- Origin, CORS, credential, content-type, upload-limit, and CSRF checks remain
  explicit edge validation.

For the production split origin, the API allows only the exact static frontend
origin and uses `credentials: include`. Cookie domain, `Secure`, `SameSite`,
session and refresh names, OAuth callback, CORS headers, and CSRF behavior come
from the production profile and are never inferred from repository naming.

## Static Frontend And MPR Styling

The Pages artifact becomes the canonical browser frontend instead of a download
landing page beside an embedded application.

- The catalog and all guides are rendered from one strict public provider
  registry.
- Every existing guide step keeps its exact first-party action, local
  screenshot, and localized alternative text.
- Data analysis is a distinct action that opens `#app/{provider}`.
- The application bundle may be public, but it cannot obtain user data without
  the protected API.
- `/config-ui.yaml` and public runtime routing are rendered from a committed,
  validated deployment profile during the repository-owned release process.
- No GitHub Actions deployment, runtime secret, or generated compatibility
  config is introduced.

This remains a mixed public site plus authenticated app in the MPR visual
language:

- `960px` centered public catalog and guide layouts;
- `1180px` authenticated workspace with a `210px` supporting rail;
- layered charcoal surfaces, thin borders, `6px` panel radii, compact controls,
  and semantic status chips;
- guide steps as restrained bordered rows, not marketing cards or a screenshot
  gallery;
- short `120ms` to `250ms` motion with reduced-motion behavior;
- no hero-led marketing layout, glass treatment, loud gradient, or oversized
  controls;
- no CSS that targets `mpr-ui` internals.

## Deployment Topology

The repository remains the owner of both production surfaces:

1. GitHub Pages publishes the static catalog, guides, authenticated application
   bundle, and browser-facing public configuration.
2. An app-owned container service publishes the protected Go API and persistent
   user workspaces through the MPR gateway.
3. The completed app-owned `.mprlab/deploy/resources.yml` will declare the
   Pages surface, TAuth tenant, container service, Caddy route, health check,
   and repository lifecycle targets.
4. Deployment implementation, Compose, Ansible, runtime examples, and runtime
   inventory live only beneath `.mprlab/deploy/`.

Repository history establishes only the former public Pages origin. Before
deployment implementation, the replacement profile must record exact literals
for:

- frontend origin;
- backend/API origin;
- browser-facing TAuth origin;
- TAuth tenant ID;
- session and refresh cookie names;
- cookie domain, `Secure`, and `SameSite`;
- OAuth callback URL;
- CORS origin and credential behavior;
- DNS owner for every hostname;
- reverse-proxy owner and upstream container port;
- persistent storage mount;
- per-user storage and active-job limits;
- the server-owned OpenAI inference origin, model identity, work budget, and
  secret references, or an explicit declaration that the OpenAI application
  cannot launch yet;
- secret references for the TAuth validator and provider services.

The existing `https://dyd.mprlab.com/` public origin may remain only if the
completed profile confirms it. No backend or TAuth hostname is guessed in this
plan.

The lifecycle remains repository-owned:

```text
make release
make publish
make deploy
```

`make release` validates and seals the Pages artifact and backend container
source identity. `make publish` publishes those exact immutable artifacts.
Only the user runs `make deploy`, which applies the committed app-owned
deployment resources. `make deploy-dry-run` remains the non-mutating validation
entrypoint.

The current macOS executable artifact, end-user operator command surface,
loopback-only production contract, embedded frontend, and Pages download
landing page are removed in the same forward change. They are not retained as
a second supported mode.

## Delivery Sequence

1. **Freeze the profile and public/private route matrix.**
   Record every required production literal and fail profile validation when
   any hosted value is absent.
2. **Protect the API and introduce `AuthenticatedUser`.**
   Construct one TAuth session validator at startup, protect every workspace
   route, inject the validated user value, and preserve public health only.
3. **Make Pages the canonical frontend.**
   Publish the provider catalog and guides as anonymous static routes, move the
   current application bundle into the Pages artifact, and replace
   `#provider/{provider}` with `#app/{provider}`.
4. **Integrate the shared shell.**
   Add `/config-ui.yaml`, `mpr-ui-config.js`, the `mpr-ui@latest` bundle marker,
   `<mpr-user>`, documented auth lifecycle listeners, synchronous early-event
   buffering, and the shared transition surface. Complete cold-session
   reconciliation only through a documented public `mpr-ui` settlement
   contract.
5. **Scope persistence by user.**
   Replace the global Netflix workspace and unscoped archive paths with
   user/provider repositories, user-specific leases and caches, bounded
   concurrency, and complete user-workspace deletion.
6. **Move Netflix behind the shared boundary.**
   Preserve its current import, progress, analytics, enrichment, export,
   replacement, and deletion contracts while proving two-user isolation.
7. **Deliver OpenAI through the same boundary.**
   Build its upload, generation, indexing, search, replacement, and deletion
   work only against the shared authenticated user contract. It must not create
   another tenant, cookie, login route, or auth state.
8. **Replace release and deployment.**
   Remove the packaged local product path, add the backend container and
   TAuth/gateway resources, and validate both public and protected surfaces.
9. **Run local and public acceptance.**
   Complete the verification ladder below before calling the authenticated
   product deployable.

## Verification Ladder

Repository and static checks:

- all catalog and guide routes render without a session;
- every guide step has one first-party action and one local screenshot;
- public routes make zero protected application API requests;
- every MPRLab library reference uses the literal `@latest` tag;
- the artifact contains no secrets, local archives, databases, vectors,
  reports, caches, or deployment-only configuration.

Real local black-box stack:

- a real TAuth service, the static frontend, API, and deterministic provider
  dependencies run together;
- a fresh browser reaches an unauthenticated guide and makes no protected API
  request;
- the shared `mpr-ui`/TAuth path authenticates once;
- the first protected request occurs only after
  `mpr-ui:auth:authenticated`;
- Netflix opens, then OpenAI opens without another login action;
- reload restores the session and requested provider application;
- explicit shared-shell sign-out clears all app-owned state;
- the protected API returns `401` without the TAuth session;
- two real test users cannot read, mutate, stream, export, or delete each
  other's provider resources.

Browser and accessibility checks:

- public and authenticated routes work at wide and narrow viewport widths;
- keyboard, focus, live status, transition, error, and reduced-motion behavior
  remain usable;
- an authenticated API `401` is shown as an integration failure and does not
  change the shell's auth state.

Production checks:

- DNS and TLS identify the expected Pages and gateway owners;
- the deployed Pages source marker matches the published release;
- the public guide works in a fresh browser without login;
- `/config-ui.yaml` matches the complete production profile;
- the backend health route reaches the expected container and port;
- CORS, `credentials: include`, cookie attributes, OAuth callback, and TAuth
  session restoration work across the deployed origins;
- a real user authenticates once, opens at least Netflix and one second
  provider application, reloads, and signs out through the shared shell.

Any failed authentication-to-workspace step blocks deployment. Localhost,
mocked auth, a healthy Pages response, or a healthy backend alone is not
production proof.

## Non-Goals

- Provider credentials or provider OAuth inside Download Your Data.
- Per-provider Download Your Data accounts or sessions.
- App-owned signup, password reset, account linking, refresh, or logout.
- Anonymous upload, analysis, search, export, or deletion.
- A compatibility path for the packaged local application or unscoped local
  data root.
- An end-user CLI that bypasses the authenticated user workspace.
- A production deploy performed by an agent.
