package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	instructionScreenshotManifestPath = "instruction-screenshots.json"
	instructionScreenshotDataPath     = "data.json"
	instructionScreenshotDirectory    = "images/instructions"
	instructionScreenshotPathPrefix   = instructionScreenshotDirectory + "/"
)

var instructionScreenshotPlatformIDs = []string{
	"openai",
	"facebook",
	"instagram",
	"linkedin",
	"tiktok",
	"x",
	"youtube",
	"google",
}

var instructionScreenshotLocaleIDs = []string{"en", "es", "fr", "ru"}

type instructionScreenshotManifest struct {
	Screenshots []instructionScreenshotEntry `json:"screenshots"`
}

type instructionScreenshotEntry struct {
	Platform              string                    `json:"platform"`
	ID                    string                    `json:"id"`
	ExpectedVisibleLabels []string                  `json:"expected_visible_labels"`
	Surface               string                    `json:"surface"`
	OutputPath            string                    `json:"output_path"`
	PixelDimensions       instructionScreenshotSize `json:"pixel_dimensions"`
	CaptureDate           string                    `json:"capture_date"`
	ReviewStatus          string                    `json:"review_status"`
}

type instructionScreenshotSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type instructionScreenshotData struct {
	InstructionScreenshots map[string][]instructionScreenshotAsset `json:"instruction_screenshots"`
	Strings                map[string]instructionScreenshotLocale  `json:"strings"`
}

type instructionScreenshotAsset struct {
	ID  string `json:"id"`
	Src string `json:"src"`
}

type instructionScreenshotLocale struct {
	Platforms []instructionScreenshotLocalizedPlatform `json:"platforms"`
}

type instructionScreenshotLocalizedPlatform struct {
	ID     string                                      `json:"id"`
	Images []instructionScreenshotLocalizedAlternative `json:"images"`
}

type instructionScreenshotLocalizedAlternative struct {
	Alt string  `json:"alt"`
	Src *string `json:"src"`
}

func TestInstructionScreenshotContract(testContext *testing.T) {
	manifest := readInstructionScreenshotJSON[instructionScreenshotManifest](
		testContext,
		instructionScreenshotManifestPath,
	)
	data := readInstructionScreenshotJSON[instructionScreenshotData](
		testContext,
		instructionScreenshotDataPath,
	)

	const expectedScreenshotCount = 12
	if len(manifest.Screenshots) != expectedScreenshotCount {
		testContext.Fatalf(
			"manifest screenshot count = %d; want %d",
			len(manifest.Screenshots),
			expectedScreenshotCount,
		)
	}

	expectedWebCounts := map[string]int{
		"openai":    0,
		"facebook":  2,
		"instagram": 2,
		"linkedin":  2,
		"tiktok":    0,
		"x":         2,
		"youtube":   2,
		"google":    2,
	}
	manifestByPlatform := make(map[string][]instructionScreenshotEntry, len(expectedWebCounts))
	seenIDs := make(map[string]struct{}, expectedScreenshotCount)
	seenPaths := make(map[string]struct{}, expectedScreenshotCount)

	for _, screenshot := range manifest.Screenshots {
		if expectedWebCounts[screenshot.Platform] != 2 {
			testContext.Fatalf("manifest contains unsupported web platform %q", screenshot.Platform)
		}
		if screenshot.ID == "" {
			testContext.Fatal("manifest contains an empty screenshot id")
		}
		if _, exists := seenIDs[screenshot.ID]; exists {
			testContext.Fatalf("manifest contains duplicate screenshot id %q", screenshot.ID)
		}
		seenIDs[screenshot.ID] = struct{}{}
		if len(screenshot.ExpectedVisibleLabels) == 0 {
			testContext.Fatalf("screenshot %q has no expected visible labels", screenshot.ID)
		}
		if screenshot.Surface != "authenticated_desktop_chrome" {
			testContext.Fatalf(
				"screenshot %q surface = %q; want authenticated_desktop_chrome",
				screenshot.ID,
				screenshot.Surface,
			)
		}
		if screenshot.ReviewStatus != "approved" {
			testContext.Fatalf("screenshot %q review status = %q; want approved", screenshot.ID, screenshot.ReviewStatus)
		}
		if _, parseError := time.Parse(time.DateOnly, screenshot.CaptureDate); parseError != nil {
			testContext.Fatalf("screenshot %q capture date is invalid: %v", screenshot.ID, parseError)
		}
		validateInstructionScreenshotPath(testContext, screenshot)
		if _, exists := seenPaths[screenshot.OutputPath]; exists {
			testContext.Fatalf("manifest contains duplicate screenshot path %q", screenshot.OutputPath)
		}
		seenPaths[screenshot.OutputPath] = struct{}{}
		validateInstructionScreenshotPNG(testContext, screenshot)
		manifestByPlatform[screenshot.Platform] = append(manifestByPlatform[screenshot.Platform], screenshot)
	}

	for platformID, expectedCount := range expectedWebCounts {
		if actualCount := len(manifestByPlatform[platformID]); actualCount != expectedCount {
			testContext.Fatalf(
				"manifest %s screenshot count = %d; want %d",
				platformID,
				actualCount,
				expectedCount,
			)
		}
	}

	directoryEntries, readDirectoryError := os.ReadDir(instructionScreenshotDirectory)
	if readDirectoryError != nil {
		testContext.Fatalf("read instruction screenshot directory: %v", readDirectoryError)
	}
	if len(directoryEntries) != expectedScreenshotCount {
		testContext.Fatalf(
			"instruction screenshot directory count = %d; want %d",
			len(directoryEntries),
			expectedScreenshotCount,
		)
	}
	for _, entry := range directoryEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".png" {
			testContext.Fatalf("instruction screenshot directory contains unexpected entry %q", entry.Name())
		}
		path := filepath.ToSlash(filepath.Join(instructionScreenshotDirectory, entry.Name()))
		if _, exists := seenPaths[path]; !exists {
			testContext.Fatalf("instruction screenshot %q is not declared in the manifest", path)
		}
	}

	validateInstructionScreenshotRegistry(testContext, data, manifestByPlatform)
	validateInstructionScreenshotLocales(testContext, data)
}

func readInstructionScreenshotJSON[valueType any](
	testContext *testing.T,
	path string,
) valueType {
	testContext.Helper()
	content, readError := os.ReadFile(path)
	if readError != nil {
		testContext.Fatalf("read %s: %v", path, readError)
	}
	var value valueType
	decoder := json.NewDecoder(bytes.NewReader(content))
	if decodeError := decoder.Decode(&value); decodeError != nil {
		testContext.Fatalf("decode %s: %v", path, decodeError)
	}
	return value
}

func validateInstructionScreenshotPath(
	testContext *testing.T,
	screenshot instructionScreenshotEntry,
) {
	testContext.Helper()
	if filepath.IsAbs(screenshot.OutputPath) ||
		filepath.ToSlash(filepath.Clean(screenshot.OutputPath)) != screenshot.OutputPath ||
		!strings.HasPrefix(screenshot.OutputPath, instructionScreenshotPathPrefix) ||
		filepath.Ext(screenshot.OutputPath) != ".png" {
		testContext.Fatalf("screenshot %q has invalid local path %q", screenshot.ID, screenshot.OutputPath)
	}
}

func validateInstructionScreenshotPNG(
	testContext *testing.T,
	screenshot instructionScreenshotEntry,
) {
	testContext.Helper()
	content, readError := os.ReadFile(screenshot.OutputPath)
	if readError != nil {
		testContext.Fatalf("read screenshot %q: %v", screenshot.ID, readError)
	}
	config, decodeError := png.DecodeConfig(bytes.NewReader(content))
	if decodeError != nil {
		testContext.Fatalf("decode screenshot %q as PNG: %v", screenshot.ID, decodeError)
	}
	if config.Width != screenshot.PixelDimensions.Width ||
		config.Height != screenshot.PixelDimensions.Height {
		testContext.Fatalf(
			"screenshot %q dimensions = %dx%d; manifest declares %dx%d",
			screenshot.ID,
			config.Width,
			config.Height,
			screenshot.PixelDimensions.Width,
			screenshot.PixelDimensions.Height,
		)
	}
	if config.Width < 480 || config.Height < 220 {
		testContext.Fatalf("screenshot %q is too small at %dx%d", screenshot.ID, config.Width, config.Height)
	}

	chunkTypes, chunkError := instructionScreenshotPNGChunkTypes(content)
	if chunkError != nil {
		testContext.Fatalf("inspect screenshot %q PNG chunks: %v", screenshot.ID, chunkError)
	}
	for _, chunkType := range chunkTypes {
		switch chunkType {
		case "IHDR", "IDAT", "IEND":
		default:
			testContext.Fatalf(
				"screenshot %q contains unexpected metadata chunk %q",
				screenshot.ID,
				chunkType,
			)
		}
	}
}

func instructionScreenshotPNGChunkTypes(content []byte) ([]string, error) {
	const pngSignatureLength = 8
	expectedSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if len(content) < pngSignatureLength || !bytes.Equal(content[:pngSignatureLength], expectedSignature) {
		return nil, fmt.Errorf("invalid PNG signature")
	}

	var chunkTypes []string
	for offset := pngSignatureLength; offset < len(content); {
		if len(content)-offset < 12 {
			return nil, fmt.Errorf("truncated PNG chunk at byte %d", offset)
		}
		chunkLength := int(binary.BigEndian.Uint32(content[offset : offset+4]))
		chunkEnd := offset + 12 + chunkLength
		if chunkEnd > len(content) {
			return nil, fmt.Errorf("PNG chunk at byte %d exceeds file length", offset)
		}
		chunkType := string(content[offset+4 : offset+8])
		chunkTypes = append(chunkTypes, chunkType)
		offset = chunkEnd
		if chunkType == "IEND" {
			if offset != len(content) {
				return nil, fmt.Errorf("data follows IEND chunk")
			}
			break
		}
	}
	if len(chunkTypes) < 3 ||
		chunkTypes[0] != "IHDR" ||
		chunkTypes[len(chunkTypes)-1] != "IEND" {
		return nil, fmt.Errorf("invalid PNG chunk sequence %v", chunkTypes)
	}
	return chunkTypes, nil
}

func validateInstructionScreenshotRegistry(
	testContext *testing.T,
	data instructionScreenshotData,
	manifestByPlatform map[string][]instructionScreenshotEntry,
) {
	testContext.Helper()
	if len(data.InstructionScreenshots) != len(instructionScreenshotPlatformIDs) {
		testContext.Fatalf(
			"screenshot registry platform count = %d; want %d",
			len(data.InstructionScreenshots),
			len(instructionScreenshotPlatformIDs),
		)
	}
	for _, platformID := range instructionScreenshotPlatformIDs {
		assets, exists := data.InstructionScreenshots[platformID]
		if !exists {
			testContext.Fatalf("screenshot registry is missing %q", platformID)
		}
		manifestScreenshots := manifestByPlatform[platformID]
		if len(assets) != len(manifestScreenshots) {
			testContext.Fatalf(
				"screenshot registry %s count = %d; manifest count = %d",
				platformID,
				len(assets),
				len(manifestScreenshots),
			)
		}
		for index, asset := range assets {
			manifestScreenshot := manifestScreenshots[index]
			if asset.ID != manifestScreenshot.ID || asset.Src != manifestScreenshot.OutputPath {
				testContext.Fatalf(
					"screenshot registry %s entry %d = %q %q; manifest = %q %q",
					platformID,
					index,
					asset.ID,
					asset.Src,
					manifestScreenshot.ID,
					manifestScreenshot.OutputPath,
				)
			}
		}
	}
}

func validateInstructionScreenshotLocales(
	testContext *testing.T,
	data instructionScreenshotData,
) {
	testContext.Helper()
	if len(data.Strings) != len(instructionScreenshotLocaleIDs) {
		testContext.Fatalf(
			"localized string count = %d; want %d",
			len(data.Strings),
			len(instructionScreenshotLocaleIDs),
		)
	}
	for _, localeID := range instructionScreenshotLocaleIDs {
		locale, exists := data.Strings[localeID]
		if !exists {
			testContext.Fatalf("localized strings are missing %q", localeID)
		}
		if len(locale.Platforms) != len(instructionScreenshotPlatformIDs)+1 {
			testContext.Fatalf(
				"locale %q platform count = %d; want %d",
				localeID,
				len(locale.Platforms),
				len(instructionScreenshotPlatformIDs)+1,
			)
		}
		if locale.Platforms[0].ID != "netflix" ||
			len(locale.Platforms[0].Images) != 0 {
			testContext.Fatalf(
				"locale %q must start with the screenshot-free Netflix workspace",
				localeID,
			)
		}
		platformByID := make(map[string]instructionScreenshotLocalizedPlatform, len(locale.Platforms))
		for _, platform := range locale.Platforms {
			if _, exists := platformByID[platform.ID]; exists {
				testContext.Fatalf(
					"locale %q contains duplicate provider %q",
					localeID,
					platform.ID,
				)
			}
			platformByID[platform.ID] = platform
		}
		for _, expectedPlatformID := range instructionScreenshotPlatformIDs {
			platform, exists := platformByID[expectedPlatformID]
			if !exists {
				testContext.Fatalf(
					"locale %q is missing provider %q",
					localeID,
					expectedPlatformID,
				)
			}
			sharedAssets := data.InstructionScreenshots[platform.ID]
			if len(platform.Images) != len(sharedAssets) {
				testContext.Fatalf(
					"locale %q %s image count = %d; want %d",
					localeID,
					platform.ID,
					len(platform.Images),
					len(sharedAssets),
				)
			}
			for imageIndex, alternative := range platform.Images {
				if strings.TrimSpace(alternative.Alt) == "" {
					testContext.Fatalf(
						"locale %q %s image %d has an empty alternative",
						localeID,
						platform.ID,
						imageIndex,
					)
				}
				if alternative.Src != nil {
					testContext.Fatalf(
						"locale %q %s image %d owns a screenshot path",
						localeID,
						platform.ID,
						imageIndex,
					)
				}
			}
		}
	}
}
