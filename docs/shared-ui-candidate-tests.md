# Shared UI Candidate Tests

The provider catalog uses `<mpr-entity-card layout="tile">` from MPR UI.
The `media`, `title`, `summary`, and `actions` slots define each provider card.
The shared component controls the action position.

## Local Validation

Set `DOWNLOAD_YOUR_DATA_MPR_UI_ROOT` to the MPR UI checkout for candidate tests.
Run the browser suite from this application checkout:

```bash
DOWNLOAD_YOUR_DATA_MPR_UI_ROOT=/path/to/mpr-ui make test-browser
```

The test runner reads the three shared assets from the selected checkout.
It records their SHA-256 hashes and supplies those assets at the browser network boundary.
An invalid path stops the test.
This test configuration keeps the production asset URLs unchanged.

Without this variable, browser tests load the production MPR UI `@latest` URLs.

## Acceptance

The catalog tests compare the actual action buttons with their card edges.
They cover five viewport widths and all four application languages.
They also verify guide keyboard navigation and the three analysis routes.

Publish the shared library with the tile layout before the application deployment.
Candidate test success does not establish CDN availability.
