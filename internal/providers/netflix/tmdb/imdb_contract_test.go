package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
)

func TestDetailsValidateOptionalIMDbTitleIDsThroughHTTP(testContext *testing.T) {
	for _, mediaType := range []netflix.MediaType{netflix.MediaTypeMovie, netflix.MediaTypeSeries} {
		for _, fixture := range []struct {
			name     string
			json     string
			expected string
			invalid  bool
		}{
			{name: "seven digits", json: `"tt0133093"`, expected: "tt0133093"},
			{name: "eight digits", json: `"tt12345678"`, expected: "tt12345678"},
			{name: "null", json: `null`},
			{name: "empty", json: `""`},
			{name: "missing"},
			{name: "name ID", json: `"nm0133093"`, invalid: true},
			{name: "short", json: `"tt123"`, invalid: true},
			{name: "whitespace", json: `" tt0133093 "`, invalid: true},
			{name: "URL", json: `"https://www.imdb.com/title/tt0133093/"`, invalid: true},
			{name: "number", json: `133093`, invalid: true},
			{name: "boolean", json: `false`, invalid: true},
			{name: "object", json: `{}`, invalid: true},
			{name: "array", json: `[]`, invalid: true},
			{name: "suffix", json: `"tt0133093/"`, invalid: true},
			{name: "uppercase", json: `"TT0133093"`, invalid: true},
			{name: "oversized", json: `"tt1234567890123456789012345678901"`, invalid: true},
		} {
			testContext.Run(string(mediaType)+"/"+fixture.name, func(testContext *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					mediaPath := "/3/movie/603"
					if mediaType == netflix.MediaTypeSeries {
						mediaPath = "/3/tv/603"
					}
					if request.URL.Path != mediaPath || request.URL.Query().Get("append_to_response") != "external_ids" {
						testContext.Errorf("details request omitted the canonical external IDs path")
					}
					externalIDs := "{}"
					if fixture.json != "" {
						externalIDs = `{"imdb_id":` + fixture.json + `}`
					}
					writer.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(writer, `{"id":603,"title":"Synthetic","name":"Synthetic","genres":[],"external_ids":%s}`, externalIDs)
				}))
				defer server.Close()
				client := newFakeServerClient(testContext, "test-token", server.URL+"/3", nil)
				details, detailsError := client.Details(context.Background(), Candidate{
					TMDBID: 603, MediaType: mediaType, Title: "Synthetic", OriginalTitle: "Synthetic",
				}, mustLocale(testContext, "en-US"))
				if fixture.invalid {
					assertTMDBErrorCode(testContext, detailsError, ErrorInvalidResponse)
					return
				}
				if detailsError != nil {
					testContext.Fatalf("decode external IMDb ID: %v", detailsError)
				}
				if fixture.expected == "" {
					if details.IMDbID != nil {
						testContext.Fatal("absent ID became an identifier")
					}
				} else if details.IMDbID == nil || details.IMDbID.String() != fixture.expected {
					testContext.Fatalf("IMDb title ID = %v; want %s", details.IMDbID, fixture.expected)
				}
			})
		}
	}
}
