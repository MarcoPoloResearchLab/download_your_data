package frontend

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"strings"
	"testing"
)

const publicSiteTestOrigin = "https://dyd.example"

func TestPublicSiteSEOContract(testContext *testing.T) {
	site, siteError := NewPublicSite(publicSiteTestOrigin)
	if siteError != nil {
		testContext.Fatalf("build public site: %v", siteError)
	}
	registry, registryError := loadResourceRegistry()
	if registryError != nil {
		testContext.Fatalf("load resource registry: %v", registryError)
	}

	expectedPathCount := len(registry.Resources) + 3
	if paths := site.Paths(); len(paths) != expectedPathCount {
		testContext.Fatalf(
			"public document path count = %d; want %d: %v",
			len(paths),
			expectedPathCount,
			paths,
		)
	}
	hub, hubType := requirePublicDocument(
		testContext,
		site,
		resourcesIndexPath,
	)
	if hubType != "text/html; charset=utf-8" ||
		!strings.Contains(hub, `<link rel="canonical" href="`+publicSiteTestOrigin+resourcesIndexPath+`">`) ||
		!strings.Contains(hub, `<h1>`+registry.Hub.H1+`</h1>`) ||
		strings.Count(hub, `class="resource-card"`) != len(registry.Resources) ||
		!strings.Contains(hub, `id="quick-verdict-title"`) ||
		!strings.Contains(hub, registry.Author.URL) ||
		!strings.Contains(hub, `datetime="`+registry.SignificantUpdate+`"`) {
		testContext.Fatalf("public resource hub is incomplete")
	}
	assertStructuredData(testContext, hub, "resource hub")

	titleSeen := make(map[string]struct{}, len(registry.Resources))
	for _, resource := range registry.Resources {
		path := resourcePath(resource.Slug)
		page, contentType := requirePublicDocument(testContext, site, path)
		if contentType != "text/html; charset=utf-8" {
			testContext.Fatalf("%s content type = %q", path, contentType)
		}
		canonicalURL := publicSiteTestOrigin + path
		if !strings.Contains(page, `<title>`+resource.MetaTitle+`</title>`) ||
			!strings.Contains(
				page,
				`<meta name="description" content="`+
					html.EscapeString(resource.MetaDescription)+`">`,
			) ||
			!strings.Contains(page, `<link rel="canonical" href="`+canonicalURL+`">`) ||
			!strings.Contains(page, `<meta property="og:url" content="`+canonicalURL+`">`) ||
			!strings.Contains(page, `<h1>`+resource.H1+`</h1>`) ||
			!strings.Contains(page, `id="quick-verdict-title"`) ||
			!strings.Contains(page, `<pre><code>`) ||
			!strings.Contains(page, `<details>`) ||
			!strings.Contains(page, `loading="lazy"`) ||
			!strings.Contains(page, registry.Author.URL) ||
			!strings.Contains(page, `datetime="`+registry.SignificantUpdate+`"`) ||
			strings.Contains(page, "noindex") ||
			strings.Contains(page, PublicOriginMarker) {
			testContext.Fatalf("public resource %q is missing an SEO contract", resource.Slug)
		}
		for _, relatedSlug := range resource.RelatedSlugs {
			if !strings.Contains(page, `href="`+resourcePath(relatedSlug)+`"`) {
				testContext.Fatalf(
					"public resource %q is missing related link %q",
					resource.Slug,
					relatedSlug,
				)
			}
		}
		if _, duplicate := titleSeen[resource.MetaTitle]; duplicate {
			testContext.Fatalf("duplicate public resource title %q", resource.MetaTitle)
		}
		titleSeen[resource.MetaTitle] = struct{}{}
		assertStructuredData(testContext, page, resource.Slug)
	}

	sitemap, sitemapType := requirePublicDocument(testContext, site, sitemapPath)
	if sitemapType != "application/xml; charset=utf-8" {
		testContext.Fatalf("sitemap content type = %q", sitemapType)
	}
	var parsedSitemap sitemapDocument
	if parseError := xml.Unmarshal([]byte(sitemap), &parsedSitemap); parseError != nil {
		testContext.Fatalf("parse sitemap: %v", parseError)
	}
	if len(parsedSitemap.URLs) != len(registry.Resources)+2 {
		testContext.Fatalf(
			"sitemap URL count = %d; want %d",
			len(parsedSitemap.URLs),
			len(registry.Resources)+2,
		)
	}
	expectedSitemapURLs := map[string]struct{}{
		publicSiteTestOrigin + "/":                {},
		publicSiteTestOrigin + resourcesIndexPath: {},
	}
	for _, resource := range registry.Resources {
		expectedSitemapURLs[publicSiteTestOrigin+resourcePath(resource.Slug)] = struct{}{}
	}
	for _, entry := range parsedSitemap.URLs {
		if _, expected := expectedSitemapURLs[entry.Location]; !expected {
			testContext.Fatalf("sitemap contains unexpected URL %q", entry.Location)
		}
		if entry.LastModified != registry.SignificantUpdate {
			testContext.Fatalf(
				"sitemap lastmod for %q = %q; want %q",
				entry.Location,
				entry.LastModified,
				registry.SignificantUpdate,
			)
		}
		delete(expectedSitemapURLs, entry.Location)
	}
	if len(expectedSitemapURLs) != 0 {
		testContext.Fatalf("sitemap is missing URLs: %v", expectedSitemapURLs)
	}

	robots, robotsType := requirePublicDocument(testContext, site, robotsPath)
	if robotsType != "text/plain; charset=utf-8" ||
		robots != "User-agent: *\nAllow: /\nSitemap: "+publicSiteTestOrigin+sitemapPath+"\n" {
		testContext.Fatalf("unexpected robots.txt: %q", robots)
	}
}

func TestPublicSiteRejectsAnUnboundOrigin(testContext *testing.T) {
	for _, publicOrigin := range []string{
		"",
		"dyd.example",
		"https://user@example.com",
		"https://dyd.example/path",
		"https://dyd.example?preview=true",
	} {
		if _, siteError := NewPublicSite(publicOrigin); siteError == nil {
			testContext.Fatalf("public origin %q was accepted", publicOrigin)
		}
	}
}

func requirePublicDocument(
	testContext *testing.T,
	site Site,
	path string,
) (string, string) {
	testContext.Helper()
	body, contentType, found := site.Read(path)
	if !found {
		testContext.Fatalf("public document %q is missing", path)
	}
	return string(body), contentType
}

func assertStructuredData(
	testContext *testing.T,
	page string,
	label string,
) {
	testContext.Helper()
	const marker = `<script id="structured-data" type="application/ld+json">`
	start := strings.Index(page, marker)
	if start < 0 {
		testContext.Fatalf("%s is missing structured data", label)
	}
	start += len(marker)
	end := strings.Index(page[start:], "</script>")
	if end < 0 {
		testContext.Fatalf("%s structured data is not closed", label)
	}
	encodedStructuredData := []byte(page[start : start+end])
	if !json.Valid(encodedStructuredData) {
		testContext.Fatalf(
			"%s structured data is invalid: %s",
			label,
			encodedStructuredData,
		)
	}
	var graph map[string]any
	if decodeError := json.Unmarshal(encodedStructuredData, &graph); decodeError != nil {
		testContext.Fatalf("%s structured data decode failed: %v", label, decodeError)
	}
	if graph["@context"] != "https://schema.org" {
		testContext.Fatalf("%s structured data has no schema.org context", label)
	}
}
