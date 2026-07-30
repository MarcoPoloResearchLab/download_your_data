package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	providerIconManifestPath = "provider-icons.json"
	providerIconDataPath     = "data.json"
	providerIconDirectory    = "images/providers"
	providerIconPathPrefix   = providerIconDirectory + "/"
)

type providerIconExpectation struct {
	ID               string
	OfficialSiteHost string
	SourceHost       string
}

var providerIconExpectations = []providerIconExpectation{
	{ID: "netflix", OfficialSiteHost: "www.netflix.com", SourceHost: "www.netflix.com"},
	{ID: "openai", OfficialSiteHost: "chatgpt.com", SourceHost: "cdn.oaistatic.com"},
	{ID: "facebook", OfficialSiteHost: "www.facebook.com", SourceHost: "www.facebook.com"},
	{ID: "instagram", OfficialSiteHost: "www.instagram.com", SourceHost: "static.cdninstagram.com"},
	{ID: "whatsapp", OfficialSiteHost: "www.whatsapp.com", SourceHost: "static.whatsapp.net"},
	{ID: "threads", OfficialSiteHost: "www.threads.com", SourceHost: "static.cdninstagram.com"},
	{ID: "linkedin", OfficialSiteHost: "www.linkedin.com", SourceHost: "www.linkedin.com"},
	{ID: "tiktok", OfficialSiteHost: "www.tiktok.com", SourceHost: "www.tiktok.com"},
	{ID: "x", OfficialSiteHost: "x.com", SourceHost: "x.com"},
	{ID: "youtube", OfficialSiteHost: "www.youtube.com", SourceHost: "www.gstatic.com"},
	{ID: "google", OfficialSiteHost: "www.google.com", SourceHost: "www.google.com"},
}

type providerIconManifest struct {
	SchemaVersion    int                 `json:"schema_version"`
	SourceReviewDate string              `json:"source_review_date"`
	Usage            string              `json:"usage"`
	RuntimeLoading   string              `json:"runtime_loading"`
	Icons            []providerIconEntry `json:"icons"`
}

type providerIconEntry struct {
	Provider        string           `json:"provider"`
	SourceKind      string           `json:"source_kind"`
	OfficialSite    string           `json:"official_site"`
	SourceURL       string           `json:"source_url"`
	OutputPath      string           `json:"output_path"`
	SHA256          string           `json:"sha256"`
	PixelDimensions providerIconSize `json:"pixel_dimensions"`
	ReviewStatus    string           `json:"review_status"`
}

type providerIconSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type providerIconData struct {
	ProviderRegistry       []providerIconRegistryEntry `json:"provider_registry"`
	InstructionScreenshots json.RawMessage             `json:"instruction_screenshots"`
	Strings                json.RawMessage             `json:"strings"`
}

type providerIconRegistryEntry struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	IconSrc string `json:"icon_src"`
}

func TestProviderIconContract(testContext *testing.T) {
	manifest := readProviderIconJSON[providerIconManifest](testContext, providerIconManifestPath)
	data := readProviderIconJSON[providerIconData](testContext, providerIconDataPath)

	if manifest.SchemaVersion != 1 {
		testContext.Fatalf("provider icon schema version = %d; want 1", manifest.SchemaVersion)
	}
	if _, parseError := time.Parse(time.DateOnly, manifest.SourceReviewDate); parseError != nil {
		testContext.Fatalf("provider icon source review date is invalid: %v", parseError)
	}
	if manifest.Usage != "provider_identification_only" || manifest.RuntimeLoading != "local_only" {
		testContext.Fatalf(
			"provider icon usage = %q/%q; want provider_identification_only/local_only",
			manifest.Usage,
			manifest.RuntimeLoading,
		)
	}
	if len(manifest.Icons) != len(providerIconExpectations) {
		testContext.Fatalf(
			"provider icon manifest count = %d; want %d",
			len(manifest.Icons),
			len(providerIconExpectations),
		)
	}
	if len(data.ProviderRegistry) != len(providerIconExpectations) {
		testContext.Fatalf(
			"provider registry count = %d; want %d",
			len(data.ProviderRegistry),
			len(providerIconExpectations),
		)
	}

	seenPaths := make(map[string]struct{}, len(providerIconExpectations))
	for index, expectation := range providerIconExpectations {
		icon := manifest.Icons[index]
		registryEntry := data.ProviderRegistry[index]
		if icon.Provider != expectation.ID || registryEntry.ID != expectation.ID {
			testContext.Fatalf(
				"provider icon position %d = manifest %q registry %q; want %q",
				index,
				icon.Provider,
				registryEntry.ID,
				expectation.ID,
			)
		}
		expectedPath := providerIconPathPrefix + expectation.ID + ".png"
		if icon.OutputPath != expectedPath || registryEntry.IconSrc != expectedPath {
			testContext.Fatalf(
				"provider %q icon paths = manifest %q registry %q; want %q",
				expectation.ID,
				icon.OutputPath,
				registryEntry.IconSrc,
				expectedPath,
			)
		}
		if _, exists := seenPaths[icon.OutputPath]; exists {
			testContext.Fatalf("provider icon manifest repeats output path %q", icon.OutputPath)
		}
		seenPaths[icon.OutputPath] = struct{}{}

		if icon.SourceKind != "first_party_site_icon" || icon.ReviewStatus != "approved" {
			testContext.Fatalf(
				"provider %q icon source/review = %q/%q; want first_party_site_icon/approved",
				expectation.ID,
				icon.SourceKind,
				icon.ReviewStatus,
			)
		}
		validateProviderIconURL(
			testContext,
			expectation.ID+" official site",
			icon.OfficialSite,
			expectation.OfficialSiteHost,
		)
		validateProviderIconURL(
			testContext,
			expectation.ID+" source",
			icon.SourceURL,
			expectation.SourceHost,
		)
		validateProviderIconFile(testContext, icon)
	}

	directoryEntries, readDirectoryError := os.ReadDir(providerIconDirectory)
	if readDirectoryError != nil {
		testContext.Fatalf("read provider icon directory: %v", readDirectoryError)
	}
	if len(directoryEntries) != len(providerIconExpectations) {
		testContext.Fatalf(
			"provider icon directory count = %d; want %d",
			len(directoryEntries),
			len(providerIconExpectations),
		)
	}
	for _, entry := range directoryEntries {
		path := filepath.ToSlash(filepath.Join(providerIconDirectory, entry.Name()))
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".png" {
			testContext.Fatalf("provider icon directory contains unexpected entry %q", entry.Name())
		}
		if _, exists := seenPaths[path]; !exists {
			testContext.Fatalf("provider icon %q is not declared in the manifest", path)
		}
	}
}

func readProviderIconJSON[valueType any](testContext *testing.T, path string) valueType {
	testContext.Helper()
	content, readError := os.ReadFile(path)
	if readError != nil {
		testContext.Fatalf("read %s: %v", path, readError)
	}
	var value valueType
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&value); decodeError != nil {
		testContext.Fatalf("decode %s: %v", path, decodeError)
	}
	return value
}

func validateProviderIconURL(
	testContext *testing.T,
	label string,
	rawURL string,
	expectedHost string,
) {
	testContext.Helper()
	target, parseError := url.Parse(rawURL)
	if parseError != nil ||
		target.Scheme != "https" ||
		target.Hostname() != expectedHost ||
		target.User != nil ||
		target.Fragment != "" {
		testContext.Fatalf("%s URL %q is not the approved first-party HTTPS boundary", label, rawURL)
	}
}

func validateProviderIconFile(testContext *testing.T, icon providerIconEntry) {
	testContext.Helper()
	if filepath.IsAbs(icon.OutputPath) ||
		filepath.ToSlash(filepath.Clean(icon.OutputPath)) != icon.OutputPath ||
		!strings.HasPrefix(icon.OutputPath, providerIconPathPrefix) ||
		filepath.Ext(icon.OutputPath) != ".png" {
		testContext.Fatalf("provider %q has invalid local icon path %q", icon.Provider, icon.OutputPath)
	}
	content, readError := os.ReadFile(icon.OutputPath)
	if readError != nil {
		testContext.Fatalf("read provider %q icon: %v", icon.Provider, readError)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != icon.SHA256 {
		testContext.Fatalf("provider %q icon digest does not match its reviewed manifest", icon.Provider)
	}
	config, decodeError := png.DecodeConfig(bytes.NewReader(content))
	if decodeError != nil {
		testContext.Fatalf("decode provider %q icon as PNG: %v", icon.Provider, decodeError)
	}
	if config.Width != icon.PixelDimensions.Width ||
		config.Height != icon.PixelDimensions.Height {
		testContext.Fatalf(
			"provider %q icon dimensions = %dx%d; manifest declares %dx%d",
			icon.Provider,
			config.Width,
			config.Height,
			icon.PixelDimensions.Width,
			icon.PixelDimensions.Height,
		)
	}
	if config.Width < 32 || config.Height < 32 {
		testContext.Fatalf("provider %q icon is too small at %dx%d", icon.Provider, config.Width, config.Height)
	}
	chunkTypes, chunkError := pngChunkTypes(content)
	if chunkError != nil {
		testContext.Fatalf("inspect provider %q icon PNG chunks: %v", icon.Provider, chunkError)
	}
	for _, chunkType := range chunkTypes {
		switch chunkType {
		case "IHDR", "IDAT", "IEND":
		default:
			testContext.Fatalf(
				"provider %q icon contains unexpected metadata chunk %q",
				icon.Provider,
				chunkType,
			)
		}
	}
}
