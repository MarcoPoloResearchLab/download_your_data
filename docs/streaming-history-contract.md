# Shared Viewing History Contract

F023 adds one private report for Netflix and Prime Video.
Open `#app/viewing-history` after shared authentication.
The workspace provides Overview, Titles, Activity, and Data sources views.
Select all services or one service with the same date and title filters.

## Inputs

Netflix accepts a profile CSV with the current `Title` and `Date` columns.
A user can attach a label to the import.
Netflix retains each supplied calendar date.

Prime accepts one ZIP archive with the exact schemas in [headers.json](../internal/providers/primevideo/headers.json).
The importer validates recognized CSV files without file extraction.
Archive text is input data.
`FileDescriptions.csv` is source documentation and does not control application behavior.

| Dataset | Source file | Role |
| --- | --- | --- |
| `viewing` | `Your Prime Video Viewing Activity/Viewing History.csv` | Primary playback timeline |
| `playback_details` | The same viewing file | Optional device and language fields |
| `watch_events` | `Your Prime Video Viewing Activity/Watch Events.csv` | Supplementary title records |
| `searches` | `Your Prime Video Viewing Activity/Search History.csv` | Search records |
| `purchases` | `Your Prime Video Library & Purchases/Purchases and Rentals.csv` | Rental and purchase records |
| `trailers` | `Your Prime Video Viewing Activity/Promotional Trailers Viewed.csv` | Promotional trailer records |

The preview shows dataset counts, UTC date coverage, and unsupported filenames.
Confirm the selected datasets before activation.
Playback details require the viewing dataset.
The active generation retains only selected records and their source references.

Limits are 64 MiB compressed, 256 MiB expanded, 128 archive entries, and 250,000 recognized records.
Each field has a 16 KiB limit.
Expanded entries above 1 MiB have a maximum compression ratio of 100.
The importer rejects unsafe paths, duplicate entries, unknown headers, and invalid source values.

## Measures

| Measure | Counting rule |
| --- | --- |
| Netflix activity | Count each supplied activity entry. |
| Prime activity | Count content playback with positive recorded seconds and no positive deletion flag. |
| Recorded watch time | Sum supplied seconds from included Prime playback records. |
| Unknown duration | Count activity and playback records that supply no seconds. |
| Zero duration | Count playback records with exactly zero supplied seconds. |
| Movies and series | Count distinct title identities in the consumption measure. |
| Episodes | Count distinct source episode identities within each service. |
| Genres, devices, and languages | Count included activities with available values. |
| Rentals and purchases | Count the selected purchase records by `Offer Type`. |
| Purchase playback evidence | Count purchase records with included Prime playback for the same title identity in the filtered data. |

Netflix activity entries and Prime playback records retain their respective units in service comparisons.
Watch Events, searches, purchases, trailers, promotions, and zero-duration records stay outside consumption totals.
Records with unknown duration or a positive deletion flag also stay outside Prime consumption totals.
An unavailable deletion flag remains unknown.
An unavailable title stays visible in Activity but does not become a distinct title.

Metadata runtime describes a title.
It does not supply recorded watch time.
Completion remains unknown.
Prime profile types distinguish adult and child activity without individual profile identifiers.
Purchase records supply counts without prices or spending totals.

UTC is the default display timezone.
Prime calendar analysis uses the selected timezone.
Netflix calendar dates remain unchanged.
Data sources shows complete selected coverage with dates in the display timezone.
Import preview dates remain in UTC.

## Title Identity And Consent

Accepted TMDB identities join title history across services.
Unresolved titles retain source identities for each provider in Titles and Top titles.
A series match joins series history without confirmation of episode identity.

Prime source identities include the source classification.
A title without series evidence and a title with series evidence have different source identities.
These identities keep each enrichment checkpoint within its applicable source evidence.

The importer removes the `Season N` suffix and keeps the season number.
Hyphens stay inside the complete series name during local analysis.
In local analysis, combined titles have no episode identity.

For these names, enrichment uses the complete name and one title interpretation that divides the name at its first hyphen.
The matcher accepts an interpretation only when it has one exact series candidate and the other interpretation has none.
Conflicting interpretations stay in review.
An accepted episode interpretation supplies the episode name for counts and export.

Enrichment requires explicit consent for title queries and the selected locale.
The server owns the TMDB credential.
Search text, dates, profile data, and source rows do not enter external queries.
The Prime matcher accepts one exact normalized title candidate with the required media type.
Ambiguous candidates remain in review.
Absent candidates remain unmatched.

`make eval-prime-matcher` checks synthetic movie, series, episode, localized-title, punctuation, and negative cases.
Its precision and recall describe those fixtures only.

## Lifecycle And Privacy

Prime and Netflix generations remain independent.
A validated replacement becomes active atomically.
The active report remains available during import or enrichment.
A repeat of the same Prime archive bytes and selection creates no duplicate activity.
Enrichment retains completed title checkpoints for explicit resume after restart.
Cancellation removes pending data and preserves the active generation.
Provider deletion removes Prime records and title results.
Complete workspace deletion removes both providers for the authenticated user.

Source files are removed after validation.
Private records remain inside the authenticated user workspace.
Logs, browser persistence, shared caches, and static artifacts exclude private records.
Route changes and sign-out cancel browser requests and remove private confirmation dialogs.

Form drafts keep import labels, files, dataset selections, and filter values through tab changes and automatic updates.
The browser keeps these drafts only in memory.
Route changes and sign-out remove the drafts.
The browser shows a created generation before upload completes.
After an upload failure, the browser retrieves provider state and keeps cancellation available.

Prime persistence uses `prime-video-library-v2` and `prime-exact-title-matcher-v2`.
The provider rejects the previous experimental persistence format.
Remove previous experimental Prime data before the source archive import.
Import the source archive again to use the current format.

## HTTP And Export

All routes below require the existing TAuth session.
Mutations require the current Origin and CSRF authorization.

| Method | Resource | Result |
| --- | --- | --- |
| `GET` | `/api/providers/prime-video` | Provider state and configured capabilities |
| `POST` | `/api/providers/prime-video/generations` | Local import or consented enrichment generation |
| `GET` | `/api/providers/prime-video/generations/{id}` | Current generation state |
| `PUT` | `/api/providers/prime-video/generations/{id}/archive` | Validated import preview |
| `PUT` | `/api/providers/prime-video/generations/{id}/selection` | Atomic activation of selected datasets |
| `DELETE` | `/api/providers/prime-video/generations/{id}` | Removal of pending generation data |
| `DELETE` | `/api/providers/prime-video` | Provider deletion with `delete-prime-video-provider` confirmation |
| `PUT` | `/api/providers/netflix/generations/{id}/profile-label` | Import label replacement |
| `GET` | `/api/viewing-history` | Filtered report with separate activity and title cursors |
| `GET` | `/api/viewing-history/export` | Complete filtered CSV |

Report filters are `provider`, `timezone`, `start_date`, `end_date`, `title`, `title_id`, `kind`, and `match_status`.
`limit` defaults to 100 and has a maximum of 200.
Activities use descending display dates and timestamps, then ascending source identities.
Titles use descending activity counts, then ascending title identities.
Cursors bind to the collection, active source revision, and filters.
A cursor for a previous source revision returns HTTP 409 with `stale_cursor`.
Malformed cursors and cursors for different filters or collections return HTTP 400 with `invalid_query`.
The browser removes both cursors when an active generation changes.
After `stale_cursor`, the browser retrieves provider state and the first report page.
Measures describe the complete filtered data rather than one page.

CSV export uses the same filters and includes every filtered source row.
It includes provider, counting unit, date precision, timezone, recorded seconds, source references, title identity, and match status.
It also retains playback flags, interval evidence, episode fields, selected playback details, and accepted metadata runtime.
Select one provider for a separate CSV.
The existing Netflix workspace and its generation export remain available at `#app/netflix`.

## Validation Boundary

Use synthetic archives and deterministic TMDB responses for committed tests.
Use authenticated HTTP and automated browsers for acceptance.
Run `make eval-netflix-matcher`, `make eval-prime-matcher`, `make test-browser`, and `make ci`.
Keep private export acceptance limited to aggregate counts.
Local acceptance does not establish publication or production availability.
