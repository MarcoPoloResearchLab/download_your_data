package httpapi

import (
	"archive/zip"
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestPrimeHTTPRejectsUnsafeArchivesAndMalformedRows(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, err := newApplicationHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		testContext.Fatal(err)
	}
	defer handler.Close()
	server := newAuthenticatedTestServer(testContext, config, handler, defaultTestUserID)
	defer server.Close()
	unsafeArchive := func(name string, content []byte) []byte {
		var output bytes.Buffer
		writer := zip.NewWriter(&output)
		entry, err := writer.Create(name)
		if err != nil {
			testContext.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			testContext.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			testContext.Fatal(err)
		}
		return output.Bytes()
	}
	scenarios := []struct {
		name    string
		archive []byte
		status  int
		code    string
	}{
		{"parent path", unsafeArchive("../escaped.csv", []byte("untrusted contents")), http.StatusUnprocessableEntity, "unsafe_archive_path"},
		{"absolute path", unsafeArchive("/escaped.csv", []byte("untrusted contents")), http.StatusUnprocessableEntity, "unsafe_archive_path"},
		{"expansion ratio", unsafeArchive("large.txt", make([]byte, 2*1024*1024)), http.StatusRequestEntityTooLarge, "limit_exceeded"},
		{"header", unsafeArchive("Your Prime Video Viewing Activity/Viewing History.csv", []byte("Title,Date\nSynthetic,2026-02-01\n")), http.StatusUnprocessableEntity, "invalid_header"},
	}
	for key, value := range map[string]string{"Seconds Viewed": "-1", "Playback Start Datetime (UTC)": "not a date", "Is Autoplay": "maybe"} {
		row := map[string]string{"Title": "Synthetic Film", "Playback Start Datetime (UTC)": "2026-02-02T01:00:00Z", "Seconds Viewed": "60", "Material Type Description": "Feature"}
		row[key] = value
		scenarios = append(scenarios, struct {
			name    string
			archive []byte
			status  int
			code    string
		}{key, syntheticPrimeArchiveWithRows(testContext, []map[string]string{row}), http.StatusUnprocessableEntity, "invalid_row"})
	}
	for _, scenario := range scenarios {
		testContext.Run(scenario.name, func(testContext *testing.T) {
			created := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations", http.MethodPost, "application/json", `{"analysis_level":"local"}`)
			var generation primeGenerationResponse
			decodeResponse(testContext, created, &generation)
			response := mutateNetflix(testContext, config, server.URL+testPrimeProviderPath+"/generations/"+generation.Generation.ID+"/archive", http.MethodPut, "application/zip", string(scenario.archive))
			assertRequestError(testContext, response, scenario.status, scenario.code)
		})
	}
}
