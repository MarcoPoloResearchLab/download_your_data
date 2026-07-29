# Per-step instruction screenshot capture

This runbook is the canonical capture contract for every provider visual listed in `instruction-screenshots.json`. Every localized instruction step must reference one approved, self-owned screenshot. The application does not support text-only steps, provider-level screenshot galleries, placeholders, mocks, unofficial tutorials, or third-party search-result images.

The same approved image may support multiple adjacent steps when it accurately shows the shared panel or first-party instructions. English, Spanish, French, and Russian reuse the same image files with localized alternative text.

## Allowed visual sources

A published visual must be one of:

- a privacy-reviewed crop of the operator’s existing authenticated provider interface; or
- a privacy-reviewed crop of the provider’s current first-party help surface when the workflow is app-only or the next provider screen would require credentials, identity verification, private account/profile selection, or an export request.

The manifest records the exact surface, official source, route, capture date, visible labels, provenance, and stop boundary. A first-party help capture must be labeled as such; it must never be presented as an authenticated app screen.

## Roles and stop conditions

The operator performs every sign-in, password, passcode, MFA, CAPTCHA, account or profile selection, and identity-verification submission. The agent may document an empty verification form when the manifest names that form as the capture boundary, but must not focus, fill, inspect, or submit it.

The agent may open only the manifest’s official source and direct route, navigate among documented read-only panels, normalize the visual state, and capture the smallest useful panel. The agent must not:

- create, request, cancel, transfer, or download an export;
- select an account or profile;
- enter or inspect credentials, codes, cookies, local storage, or browser session state;
- connect an external destination, grant a permission, or change a provider setting;
- use a mock, third-party guide, unofficial route, or search-result image;
- save browser authentication state or a reusable credential-bearing profile.

If a required authenticated screen cannot be reached without crossing a forbidden boundary, use the current first-party help surface only when it contains the exact labels and instructions needed by the step. Otherwise mark the manifest entry `blocked` and stop; do not publish a fabricated substitute.

## Required capture order

1. Reconcile every instruction, label, source, and route against the current official provider help contract.
2. Confirm the provider’s current brand, copyright, and terms guidance permits the proposed independent instructional use and record any attribution requirement.
3. Confirm the declared surface is available without crossing its forbidden boundary.
4. For authenticated desktop captures, use English at a consistent desktop viewport and preserve the provider’s current appearance.
5. Navigate only to the allowlisted panel and stop before the forbidden action.
6. Hide animation, focus carets, transient notifications, browser chrome, and unrelated account content.
7. Exclude every personal identifier through cropping or a flat opaque capture-time mask. Blur is not acceptable.
8. Capture into an owner-only temporary directory outside the repository.
9. Review at full resolution, publish only the approved derivative, then normalize it with `go run ./scripts/normalize-instruction-pngs <path>`.
10. Re-open the published derivative at full resolution and verify privacy, provenance, current labels, and legibility.
11. Delete the temporary capture directory after the approved derivatives are present.

## Privacy review

Reject a derivative containing names, handles, email addresses, phone numbers, avatars, organizations, locations, account identifiers, notifications, private counts, selected account/profile details, archive history, credentials, verification material, browser tabs, address bars, extensions, bookmarks, or downloads.

Published PNGs may contain only the structural `IHDR`, `IDAT`, and `IEND` chunks. Crops must retain enough provider context and exact labels to teach the mapped step.

Provider-specific constraints:

- Google and LinkedIn interface pixels remain unaltered; exclude account headers and member content by cropping.
- OpenAI, Facebook, Instagram, X, Netflix, WhatsApp, and TikTok captures remain accurate, descriptive, subordinate to the instructions, and visibly unaffiliated.
- WhatsApp and TikTok first-party help captures are public help surfaces, not substitutes presented as authenticated mobile app screens.
- Threads may reuse the Instagram Accounts Center captures because that is the current first-party export surface named by Meta.

## Acceptance

The set is accepted only when:

- every provider has at least one approved screenshot;
- every instruction step in every locale contains non-empty text, one valid screenshot ID, and localized alternative text;
- every provider screenshot is used by at least one step and every manifest screenshot is referenced;
- no provider-level screenshot gallery, text-only exception, placeholder, mock, or locale-specific duplicate survives;
- every metadata-free PNG exists beneath `images/instructions/`;
- desktop and mobile browser coverage proves each step renders its visual beside the instruction without overflow;
- `make validate-instruction-screenshots`, `make test-browser`, and `make ci` pass.

Provider names, trademarks, help content, and interfaces remain the property of their respective owners. Publication is an independent instructional reference and does not imply affiliation or endorsement.
