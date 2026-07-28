# Authenticated instruction screenshot capture

This runbook is the only capture contract for the authenticated web screenshots listed in `instruction-screenshots.json`. It permits an agent to prepare provider-interface screenshots from the operator's existing Chrome session without receiving credentials, changing account state, requesting an archive, or retaining personal information. Authenticated TikTok mobile capture is independently owned by `I008`.

## Roles and stop conditions

The operator performs every sign-in, password, passcode, MFA, CAPTCHA, account or profile selection, and identity-verification submission. The agent may document an empty verification form when the manifest names that form as the capture boundary, but must not focus, fill, inspect, or submit it.

The agent may open only the manifest's official source and direct route, navigate among the documented read-only panels, normalize the visual state, add opaque capture-time masks, and capture the smallest useful panel. The agent must not:

- create, request, cancel, transfer, or download an export;
- select an account or profile;
- enter or inspect credentials, codes, cookies, local storage, or browser session state;
- connect an external destination, grant a permission, or change a provider setting;
- use an unofficial web flow, mock, third-party guide, or search-result image as a substitute;
- save a browser authentication state or reusable credential-bearing profile.

If the required authenticated web surface cannot be reached without crossing a forbidden boundary, mark that manifest entry `blocked`, record the exact boundary in `review_note`, and stop. A partial web screenshot set is not publishable.

## Required capture order

1. Reconcile the manifest's labels, instructions, source, and route against the current official provider help page.
2. Confirm the provider's current brand, copyright, and terms guidance permits the proposed independent instructional use and record any attribution requirement in the manifest.
3. Confirm the entry's authenticated surface is already available; stop for operator input if authentication is requested.
4. Set desktop Chrome to English, a 1440×1000 CSS-pixel viewport, 100% zoom, and device scale factor 1. Preserve the provider's current authenticated appearance instead of changing an account-level theme preference for capture.
5. Navigate only to the entry's allowlisted panel and stop before its forbidden action.
6. Hide animation, focus carets, transient notifications, browser chrome, and unrelated account content.
7. Cover every personal identifier with an opaque capture-time mask or exclude it through cropping. Blur is not acceptable.
8. Capture into an owner-only temporary directory outside the repository.
9. Review the capture at full resolution, strip metadata, and publish the approved derivative at the manifest path.
10. Re-open the published derivative at full resolution and approve it only after both privacy and provenance checks pass.
11. Delete the temporary capture directory after all approved derivatives are present.

## Private temporary storage

Create one private directory per capture run:

```bash
capture_root="$(mktemp -d -t download-your-data-i007.XXXXXX)"
chmod 700 "${capture_root}"
```

Do not place raw captures, browser profiles, authentication state, cookies, downloads, or personal archives anywhere in this repository. Do not commit the temporary path because it may reveal operator-local details.

## Privacy review

At full resolution, reject a derivative containing any of the following:

- names, handles, emails, phone numbers, avatars, organizations, locations, or account identifiers;
- notifications, private counts, selected account/profile details, or archive history;
- passwords, filled credential fields, verification codes, CAPTCHA content, account recovery details, or other secret identity-verification material;
- browser tabs, address bar, extensions, bookmarks, downloads, or browser chrome;
- image metadata beyond the structural PNG chunks required to decode the image.

Masks must be flat opaque shapes that do not preserve readable edges. Crops must retain enough provider navigation context and visible labels to teach the manifest purpose.

Provider-specific publication rules narrow that general masking rule:

- Google allows unaltered static product screenshots for educational materials. Exclude the account header by cropping and do not mask or otherwise change product-interface pixels.
- LinkedIn allows screenshots for instructive, educational, or illustrative purposes when their appearance is unchanged and no other member is identifiable. Exclude the account header and any member content by cropping; do not superimpose a mask on LinkedIn interface pixels.
- Instagram does not require a permission request for this non-broadcast, non-radio, non-out-of-home, standard-size digital instructional use, but the interface and brand must remain accurate and must not imply endorsement.
- Facebook and X captures must remain accurate, descriptive, subordinate to the product instructions, and visibly unaffiliated.
- TikTok mobile publication is outside this web set and remains owned by `I008`.

## Acceptance

The web set is accepted only when all twelve entries are `approved`, all twelve metadata-free PNGs exist beneath `images/instructions/`, every locale maps the six web platforms to the same two screenshot IDs, and the repository validator plus desktop and mobile application browser coverage pass. TikTok remains text-only until `I008`; it must not render an empty image placeholder.

The screenshots are independent instructional references. Provider names, trademarks, and interfaces remain the property of their respective owners; publication does not imply affiliation or endorsement. The manifest records the official capture source, rights-review source, capture date, and required attribution for every image.
