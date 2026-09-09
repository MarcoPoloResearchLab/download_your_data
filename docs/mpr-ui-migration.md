# I017 Shared Authentication Migration

I017 prepares Download Your Data for the mpr-ui I009 provider map.
The source starts from `ea1f1f179a3e645a9785a5e36ba7e2b763159ab4` and preserves existing tracker edits.
The canonical serializer supplies both the API response and the Pages artifact.
Its current provider map retains configured origins, tenant identity, Google identifiers, and session endpoints.
The application retains its anonymous guides, lifecycle buffer, and private workspace boundary.
The footer uses legal-slot content and has no obsolete menu input.

## Validation

Baseline native CI passed, including production artifacts and both existing browser suites.
The new HTTP regression first rejected the flat authentication configuration.
After configuration migration, the browser regression failed when a protected read expired.
Shared request transport corrected that failure.
Four auth flows now pass across two viewport widths and both origin configurations.
They cover Google exchange, restored sessions, read recovery, mutation replay, logout, and footer content.
The new browser scenario uses real application files and API operations.
Google and TAuth are controlled at their external boundaries.
The shared candidate is `768f25936497c5aabd426197d21c2100b6e5d9a1`.
The native browser harness verifies all three asset digests before application navigation.
Focused checks and both existing browser suites passed.
The browser harness supplies controlled Google and nonce responses for all shared-library checks.
Application navigation uses DOM readiness and its existing rendered-state waits.
The broader suite exposed shared header overflow after Google startup failure beside application controls.
The final B069 candidate includes the shared B068 error-state correction.
Final B069 native CI passed, including Go tests, static checks, lifecycle checks, production artifacts, and browser suites.
The final validation log is `/tmp/dyd-i017-b069-ci.log`.
The repository has no hosted workflow. Local CI supplies the required source validation.

## Publication Gates

GitHub confirms the `gh-pages` branch and `dyd.mprlab.com` domain.
The manifest already declares the Pages, API, and TAuth resources.
The [public asset record](mpr-ui/public-assets-2026-09-09.json) contains eight successful responses from one network location.
Pages and the API returned identical configuration bytes.
Pages declares `max-age=600`. The API config response declares `no-store`.
Shared assets declare `max-age=604800` and `s-maxage=43200`.
These observations establish current responses and cache lifetimes only.

Prepare the maintenance artifact and verify cache transitions before shared publication.
Qualify one final immutable shared candidate across all affected applications.
Verify the released Pages marker, real Google login, production cookies, and private operations after activation.
The user owns release, publication, and deployment.
