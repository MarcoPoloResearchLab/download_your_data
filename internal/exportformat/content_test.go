package exportformat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractContentIgnoresAssetPointers(testContext *testing.T) {
	rawContent := json.RawMessage(`{
        "content_type": "multimodal_text",
        "parts": [
            {"content_type": "image_asset_pointer", "asset_pointer": "file-service://secret"},
            "What does berth mean?"
        ]
    }`)
	extractedContent := ExtractContent(rawContent)
	if extractedContent.Text != "What does berth mean?" {
		testContext.Fatalf("unexpected extracted text: %q", extractedContent.Text)
	}
	if strings.Contains(extractedContent.Text, "file-service") {
		testContext.Fatal("asset pointer should not be included")
	}
}
