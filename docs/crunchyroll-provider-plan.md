# Crunchyroll Viewing History

Upload a captured Crunchyroll profile-history CSV in the [shared viewing history workspace](streaming-history-contract.md). This import uses the CSV format established by the recovered export. Sony's privacy request is a separate acquisition workflow with an independently validated input format when a sample becomes available. [Sony privacy policy](https://www.sonypictures.com/corp/privacy.html)

## Recovered Profile Export

The earlier export used an existing signed-in browser session. It opened History for the selected profile, scrolled while older rows loaded, and collected the visible rows into CSV. The first pass selected films and theatrical specials and checked titles against the catalog. The second pass kept the entire visible list, including episodes. Crunchyroll documents History as a profile feature. [Crunchyroll website help](https://help.crunchyroll.com/hc/en-us/articles/22728708616852-Getting-started-on-Crunchyroll-website)

The recovered full-history CSV contains 934 distinct rows. Its coverage describes one visible profile at the time of capture. The service's retention period and the CSV's coverage of repeat views remain unknown.

| CSV field | Archive evidence |
| --- | --- |
| `history_date` | Calendar date. Keep the captured date across display timezones. |
| `series_or_collection` | Group label captured from the page. A season number remains unknown unless the source supplies it. |
| `title` | Display title of the visible item. Preserve the captured text. |
| `crunchyroll_status` | Source badge text. The CSV includes `Watched`, time-left labels, and bare durations. Preserve the text and leave recorded watch time and completion unknown. |
| `format` | The export's classification of the item as `Episode`, `Movie`, or `Theatrical special`. |
| `crunchyroll_url` | The Crunchyroll watch link. Its watch ID identifies the item within Crunchyroll. Event identity remains unknown. |

Crunchyroll lets users mark items as watched. Count this input in visible history entries and report its date range, profile, and capture date. Keep activity from the Prime Video app in the Amazon source and compare the coverage of the two sources independently. [Crunchyroll watched status](https://help.crunchyroll.com/hc/en-us/articles/31320247305748-How-do-I-mark-episodes-seasons-and-shows-as-watched), [Crunchyroll on Prime Video](https://help.crunchyroll.com/article/crunchyroll-channel-available-on-amazon-prime)

## First-Party Request

On November 27, 2026, the Sony form displayed **Copy of Personal Information** and **Crunchyroll** after selecting California. It offers **A consumer** and **An authorized representative of a consumer**. Choose the user's actual jurisdiction and use the options available there. For residents outside the United States, start from Sony's international privacy policy. [Sony request form](https://privacyportal-cdn.onetrust.com/dsarwebform/d19e506f-1a64-463d-94e4-914dd635817d/fc46fc0c-407c-4735-9a43-14dfa283d17c.html), [Sony international privacy policy](https://intl.sonypictures.com/en/privacy-policy)

1. Prepare a copy request for retained Crunchyroll viewing history for the approved profiles and date range. When the form or follow-up permits detail, ask for a machine-readable copy with the available viewing dates or timestamps, title and episode identifiers, and playback status or progress.
2. Have the account holder submit the request, or support representative submission in an eligible jurisdiction with signed permission for that specific request. Review the account, request scope, and submission destination with the user before submitting. Keep the request reference and status for follow-up.
3. Leave identity verification with the account holder on Sony's surface. The form uses reCAPTCHA. Sony can require the user to sign in or verify personal information directly. California rules allow a business to require proof of signed authorization for an agent. [Sony privacy policy](https://www.sonypictures.com/corp/privacy.html), [California privacy regulations, section 7063](https://cppa.ca.gov/regulations/pdf/ccpa_statute_eff_20260101.pdf)
4. Have the user provide the returned file to their private archive. Use representative delivery only after verifying that Sony supports it and that the user's permission covers receipt. Treat file receipt and request submission as separate operations.

This supports one-time delegated acquisition with user verification. The user can authorize the request without giving Download Your Data access to their Crunchyroll credentials. Direct automated collection requires a provider-approved integration. Section 5 of the current Terms of Service restricts automated data gathering, and public delegated access to viewing history remains unverified. [Crunchyroll Terms of Service](https://www.crunchyroll.com/tos/)

## Archive Import

Select the CSV in the shared file picker. The workspace detects the six-column header, validates the file, and adds its rows to the Crunchyroll source. Select **Crunchyroll** to filter the timeline and CSV export. The file picker replaces the active Crunchyroll snapshot, and data management provides independent deletion.

The current source represents the supplied snapshot. The workspace displays its covered dates with the history-only counting unit. Each row retains the source collection, item format, status, and watch link. The import stores the validated structured rows in the signed-in user's workspace.

Local authenticated HTTP acceptance imported and re-exported all 934 rows, including 817 marked `Watched`. The repository tests use synthetic data.

For the separate Sony acquisition workflow, obtain one completed privacy response and check the viewing-history files, profiles, date range, playback status, and time precision. Compare its aggregate coverage with the visible-profile capture before defining a second input contract.
