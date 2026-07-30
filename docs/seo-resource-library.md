# SEO resource library

## Purpose

The public resource library turns current repository-owned provider guidance
into useful, path-based pages that can be discovered and understood without
executing the hash-routed application. The pages do not replace the
interactive guides. They explain the export decision, preserve first-party
steps and reviewed visuals, name limitations, and link visitors to the exact
guide or supported workspace action.

## Current scope

`/resources/` links twelve distinct resources:

| Resource | Primary intent | Current product destination |
| --- | --- | --- |
| Netflix viewing history CSV | Download the supported per-profile CSV | `#guide/netflix` |
| Netflix viewing history analyzer | Analyze the supported CSV privately | `#app/netflix` |
| ChatGPT data export | Request and save the OpenAI ZIP | `#guide/openai` |
| WhatsApp chat export | Choose account report or per-chat export | `#guide/whatsapp` |
| Google Takeout | Export selected Google products | `#guide/google` |
| YouTube data export | Export YouTube through focused Takeout | `#guide/youtube` |
| X data archive | Complete X verification and archive download | `#guide/x` |
| TikTok data export | Request and retrieve the mobile archive | `#guide/tiktok` |
| LinkedIn data export | Choose categories or the larger archive | `#guide/linkedin` |
| Facebook data export | Scope an Accounts Center export | `#guide/facebook` |
| Instagram data export | Scope an Instagram Accounts Center export | `#guide/instagram` |
| Threads data export | Export Threads through Instagram Accounts Center | `#guide/threads` |

The cluster intentionally does not publish a ChatGPT browser-import page,
full-Netflix-archive analyzer, or mandatory-TMDB page. Those claims do not
match the current product. TMDB enrichment remains an optional, consent-based
section of the Netflix analyzer resource.

## Evidence and content ownership

The validated registry is
`frontend/content/resources.json`. Rendering joins that SEO-specific content
with:

- provider order, names, guide steps, official references, and current
  limitations from `frontend/content/application.json`;
- screenshot dimensions, review status, and attribution from
  `frontend/manifests/instruction-screenshots.json`;
- the selected runtime public origin from
  `DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN`.

Every resource includes a visible quick verdict, repository snippet, author
profile, significant-content date, limitations, first-party sources, semantic
FAQ, and related-resource links. Screenshots are deduplicated per page,
lazy-loaded below the fold, and rendered with explicit dimensions.

## Indexing contract

- The canonical resource form is `/resources/{slug}/`.
- Canonical, Open Graph, JSON-LD, sitemap, and internal URLs use the same
  trailing-slash form.
- `/resources` redirects to `/resources/`; sitemap entries use only the final
  `200` URL.
- `sitemap.xml` contains the public root, resource hub, and current resource
  pages. It does not contain hash fragments, protected APIs, or unknown
  production values.
- Every new sitemap URL uses the explicit `2026-07-30` significant-content
  creation date from the registry. Builds do not replace it with build or
  deployment time.
- `robots.txt` allows public crawling and references the absolute sitemap URL.
- The application footer links the resource hub with a crawlable HTML anchor;
  the hub links every resource, and every resource links adjacent pages.

## Verification

The frontend contract validates the registry, provider coverage, metadata
bounds, screenshot readiness, internal relations, structured data, sitemap
URLs, and truthful `<lastmod>` values. The HTTP contract requests every public
document, follows every sitemap path without a redirect, checks the canonical
origin, and confirms slash normalization. Browser coverage checks the hub and
representative resources at wide and narrow viewport widths and asserts that
public resource browsing makes no protected API request.

After the production profile is frozen and deployed, verify representative
resource URLs with Google Search Console URL Inspection and validate the
rendered JSON-LD with Google's Rich Results Test. Deployment is a separate
user-owned lifecycle and is not implied by local validation.

## SEO Resources evaluation

Status: **needs revision before publication; source implementation passes**.
The only below-threshold category is live Google indexing readiness because
the exact production profile is still unresolved and no deployment was
authorized or performed.

| Category | Score | Evidence |
| --- | ---: | --- |
| Repo grounding | 5 | Provider steps, limitations, screenshots, product capabilities, and snippets are joined from validated repository sources. |
| Use-case specificity | 5 | Each page has a distinct export path, input, timing, objection set, FAQ, and CTA. |
| Doorway-page safety | 4 | The shared presentation is consistent, while provider workflows and the Netflix analysis job remain substantively different. |
| SEO metadata quality | 5 | Titles and descriptions are unique, bounded, intent-aligned, and contract-tested. |
| Keyword naturalness | 5 | Primary terms appear in titles, headings, and introductions without repeated keyword blocks. |
| Factual integrity | 5 | Unsupported importers, full-archive analysis, mandatory enrichment, proof, volume, and ranking claims are excluded. |
| Conversion clarity | 5 | Every resource opens its exact visual guide or the supported Netflix analysis route. |
| Duplicate-content risk | 4 | Provider-specific steps, constraints, output, FAQ, visuals, and related paths keep overlap bounded. |
| Site integration and discoverability | 5 | Root footer, hub, every resource, related-resource links, sitemap, and robots form a complete crawl path. |
| Google indexing readiness | 3 | Local canonical, redirect, sitemap, robots, JSON-LD, and `200` contracts pass; final hosted origin and post-deploy inspection remain unavailable. |
| Handoff quality | 5 | One registry, reusable renderer, explicit source paths, docs, and black-box tests preserve the implementation decisions. |

Required publication follow-up:

1. Freeze the exact static frontend production origin and replacement release
   workflow.
2. Materialize and deploy the validated site against that origin through the
   user-owned lifecycle.
3. Confirm representative final URLs return `200` without a canonical redirect.
4. Submit `sitemap.xml`, inspect representative pages in Search Console, and
   run the rendered structured data through Rich Results Test.
