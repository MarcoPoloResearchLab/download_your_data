package exportformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ExtractedContent struct {
	ContentType string
	Text        string
	Warnings    []string
}

func ExtractContent(rawContent json.RawMessage) ExtractedContent {
	result := ExtractedContent{}
	trimmedContent := bytes.TrimSpace(rawContent)
	if len(trimmedContent) == 0 || bytes.Equal(trimmedContent, []byte("null")) {
		return result
	}

	var decodedContent any
	if err := json.Unmarshal(trimmedContent, &decodedContent); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("content JSON could not be decoded: %v", err))
		return result
	}

	if contentObject, isObject := decodedContent.(map[string]any); isObject {
		if contentType, isString := contentObject["content_type"].(string); isString {
			result.ContentType = contentType
		}
	}

	collectedText := make([]string, 0, 4)
	collectText(decodedContent, 0, &collectedText)
	result.Text = strings.TrimSpace(strings.Join(removeEmptyStrings(collectedText), "\n"))
	return result
}

func collectText(value any, depth int, collectedText *[]string) {
	if depth > 10 || value == nil {
		return
	}

	switch typedValue := value.(type) {
	case string:
		trimmedValue := strings.TrimSpace(typedValue)
		if trimmedValue != "" {
			*collectedText = append(*collectedText, trimmedValue)
		}
	case []any:
		for _, item := range typedValue {
			collectText(item, depth+1, collectedText)
		}
	case map[string]any:
		contentType, _ := typedValue["content_type"].(string)
		if shouldIgnoreContentType(contentType) {
			return
		}

		preferredKeys := []string{"parts", "text", "transcript", "result", "content"}
		for _, preferredKey := range preferredKeys {
			preferredValue, exists := typedValue[preferredKey]
			if !exists {
				continue
			}
			collectText(preferredValue, depth+1, collectedText)
		}
	}
}

func shouldIgnoreContentType(contentType string) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(contentType))
	switch normalizedType {
	case "image_asset_pointer", "audio_asset_pointer", "file", "file_asset_pointer", "asset_pointer":
		return true
	default:
		return false
	}
}

func removeEmptyStrings(values []string) []string {
	filteredValues := make([]string, 0, len(values))
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			filteredValues = append(filteredValues, trimmedValue)
		}
	}
	return filteredValues
}
