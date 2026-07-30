package httpapi

import (
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
)

type publicSitemapContract struct {
	URLs []publicSitemapURLContract `xml:"url"`
}

type publicSitemapURLContract struct {
	Location     string `xml:"loc"`
	LastModified string `xml:"lastmod"`
}

func TestPublicResourceHTTPContract(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()

	publicSite, siteError := frontend.NewPublicSite(
		config.Authentication().PublicOrigin(),
	)
	if siteError != nil {
		testContext.Fatalf("build expected public site: %v", siteError)
	}
	for _, path := range publicSite.Paths() {
		response, requestError := http.Get(server.URL + path)
		if requestError != nil {
			testContext.Fatalf("request public document %s: %v", path, requestError)
		}
		body, readError := io.ReadAll(response.Body)
		response.Body.Close()
		if readError != nil {
			testContext.Fatalf("read public document %s: %v", path, readError)
		}
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf(
				"public document %s status = %d; want %d",
				path,
				response.StatusCode,
				http.StatusOK,
			)
		}
		if response.Header.Get("Cache-Control") != "public, max-age=300" ||
			response.Header.Get("Content-Security-Policy") != buildContentSecurityPolicy(config) {
			testContext.Fatalf("public document %s has incomplete public headers", path)
		}
		if strings.HasSuffix(path, "/") &&
			(!strings.Contains(string(body), `rel="canonical"`) ||
				!strings.Contains(string(body), config.Authentication().PublicOrigin()+path) ||
				strings.Contains(string(body), frontend.PublicOriginMarker)) {
			testContext.Fatalf("public HTML %s has invalid canonical output", path)
		}
	}

	rootResponse, rootError := http.Get(server.URL + "/")
	if rootError != nil {
		testContext.Fatalf("request public root: %v", rootError)
	}
	rootBody, rootReadError := io.ReadAll(rootResponse.Body)
	rootResponse.Body.Close()
	if rootReadError != nil {
		testContext.Fatalf("read public root: %v", rootReadError)
	}
	if rootResponse.StatusCode != http.StatusOK ||
		!strings.Contains(
			string(rootBody),
			`<link rel="canonical" href="`+config.Authentication().PublicOrigin()+`/">`,
		) ||
		!strings.Contains(string(rootBody), `href="/resources/"`) ||
		strings.Contains(string(rootBody), frontend.PublicOriginMarker) {
		testContext.Fatalf("public root is missing canonical resource discovery")
	}

	noRedirectClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, path := range publicSite.Paths() {
		if !strings.HasSuffix(path, "/") {
			continue
		}
		slashlessPath := strings.TrimSuffix(path, "/")
		noSlashResponse, noSlashError := noRedirectClient.Get(
			server.URL + slashlessPath,
		)
		if noSlashError != nil {
			testContext.Fatalf(
				"request slashless public path %s: %v",
				slashlessPath,
				noSlashError,
			)
		}
		noSlashResponse.Body.Close()
		if noSlashResponse.StatusCode != http.StatusPermanentRedirect ||
			noSlashResponse.Header.Get("Location") != path {
			testContext.Fatalf(
				"slashless path %s response = %d %q; want 308 %s",
				slashlessPath,
				noSlashResponse.StatusCode,
				noSlashResponse.Header.Get("Location"),
				path,
			)
		}
	}

	unknownResponse, unknownError := http.Get(server.URL + "/resources/not-a-current-resource/")
	if unknownError != nil {
		testContext.Fatalf("request unknown resource: %v", unknownError)
	}
	unknownResponse.Body.Close()
	if unknownResponse.StatusCode != http.StatusNotFound {
		testContext.Fatalf(
			"unknown resource status = %d; want %d",
			unknownResponse.StatusCode,
			http.StatusNotFound,
		)
	}

	sitemapResponse, sitemapError := http.Get(server.URL + "/sitemap.xml")
	if sitemapError != nil {
		testContext.Fatalf("request sitemap: %v", sitemapError)
	}
	var sitemap publicSitemapContract
	if decodeError := xml.NewDecoder(sitemapResponse.Body).Decode(&sitemap); decodeError != nil {
		sitemapResponse.Body.Close()
		testContext.Fatalf("decode sitemap: %v", decodeError)
	}
	sitemapResponse.Body.Close()
	if len(sitemap.URLs) < 3 {
		testContext.Fatalf("sitemap contains too few URLs: %+v", sitemap.URLs)
	}
	for _, entry := range sitemap.URLs {
		parsedLocation, parseError := url.Parse(entry.Location)
		if parseError != nil {
			testContext.Fatalf("parse sitemap URL %q: %v", entry.Location, parseError)
		}
		if parsedLocation.Scheme+"://"+parsedLocation.Host !=
			config.Authentication().PublicOrigin() ||
			entry.LastModified == "" {
			testContext.Fatalf("sitemap entry is not canonical and dated: %+v", entry)
		}
		pageResponse, pageError := noRedirectClient.Get(server.URL + parsedLocation.Path)
		if pageError != nil {
			testContext.Fatalf("request sitemap path %s: %v", parsedLocation.Path, pageError)
		}
		pageResponse.Body.Close()
		if pageResponse.StatusCode != http.StatusOK {
			testContext.Fatalf(
				"sitemap path %s status = %d; want 200",
				parsedLocation.Path,
				pageResponse.StatusCode,
			)
		}
	}

	assertPublicHeadResponse(testContext, server.URL+"/resources/")
	assertPublicHeadResponse(testContext, server.URL+"/sitemap.xml")
}

func assertPublicHeadResponse(testContext *testing.T, requestURL string) {
	testContext.Helper()
	request, requestError := http.NewRequest(http.MethodHead, requestURL, nil)
	if requestError != nil {
		testContext.Fatalf("create HEAD request: %v", requestError)
	}
	response, responseError := http.DefaultClient.Do(request)
	if responseError != nil {
		testContext.Fatalf("perform HEAD request: %v", responseError)
	}
	body, readError := io.ReadAll(response.Body)
	response.Body.Close()
	if readError != nil {
		testContext.Fatalf("read HEAD response: %v", readError)
	}
	if response.StatusCode != http.StatusOK || len(body) != 0 {
		testContext.Fatalf(
			"HEAD %s response = %d with %d body bytes",
			requestURL,
			response.StatusCode,
			len(body),
		)
	}
}
