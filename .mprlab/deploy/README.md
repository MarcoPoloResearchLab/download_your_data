# Deployment contract

Download Your Data is now one split web product:

1. A static public catalog, provider-guide, Credits, and application bundle.
2. A protected Go API whose provider resources are owned by one authenticated
   TAuth user.

The previous macOS application archive and GitHub Pages download landing page
are retired. They must not be released or deployed again.

## Lifecycle

The repository retains the canonical lifecycle entrypoints:

```bash
make release
make publish
make deploy
```

All three, plus `make deploy-dry-run`, currently fail closed. They remain
blocked until the exact production profile and replacement release workflow
are committed. Only the user runs `make deploy` after the non-mutating checks
pass.

## Required production profile

The existing repository state confirms only the historical public origin
`https://dyd.mprlab.com/`. It does not establish that this origin remains
current, and it provides no authenticated backend profile. Before deployment
work can resume, the app-owned profile must record exact, reviewed literals
for:

- static frontend origin and DNS/TLS owner;
- protected API origin, container image, upstream port, health path, and
  persistent user-storage mount;
- browser-facing TAuth origin and tenant;
- session and refresh cookie names, cookie domain, `Secure`, and `SameSite`;
- OAuth callback URL and Google client ID;
- exact credentialed CORS origin and CSRF behavior;
- OpenAI inference origin, boundary, models, work budget, and secret
  references;
- TAuth signing-key, TMDB, inference, and other runtime secret references;
- per-user storage, generation, upload, and active-job limits.

Do not infer a backend hostname, TAuth tenant, cookie name, callback, port,
mount, or secret reference from repository naming.

## Target resources

After the profile is frozen, `.mprlab/deploy/resources.yml` must declare and
the repository must implement:

- the static Pages frontend;
- the TAuth tenant dependency;
- the Go API container and persistent storage;
- the MPR gateway/Caddy route and public health check;
- immutable `release`, `publish`, user-owned `deploy`, and non-mutating
  `deploy-dry-run` targets.

The gateway discovers and dispatches this app-owned workflow. It does not
supply missing production values or own this repository's resources.
