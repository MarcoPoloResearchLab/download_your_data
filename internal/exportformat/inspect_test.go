package exportformat

import (
	"io"
	"strings"
	"testing"
)

func TestInspectReportsRepeatedSourceMessageOccurrences(testContext *testing.T) {
	exportJSON := `[
  {
    "id": "conversation-a",
    "mapping": {
      "node-1": {
        "message": {
          "id": "shared-message-id",
          "author": {"role": "user"},
          "content": {"content_type": "text", "parts": ["define berth"]}
        }
      },
      "node-2": {
        "message": {
          "id": "shared-message-id",
          "author": {"role": "assistant"},
          "content": {"content_type": "text", "parts": ["definition"]}
        }
      },
      "node-3": {
        "message": {
          "id": "another-message-id",
          "author": {"role": "user"},
          "content": {"content_type": "text", "parts": ["define wily"]}
        }
      },
      "node-4": {
        "message": {
          "author": {"role": "assistant"},
          "content": {"content_type": "text", "parts": ["definition"]}
        }
      }
    }
  }
]`
	collection := &SourceCollection{
		Sources: []ConversationSource{{
			Name: "conversations.json",
			Open: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(exportJSON)), nil
			},
		}},
		Close: func() error { return nil },
	}

	inspection, inspectError := InspectSources(collection)
	if inspectError != nil {
		testContext.Fatalf("inspect fixture: %v", inspectError)
	}
	if inspection.Messages != 4 {
		testContext.Fatalf("expected 4 messages, received %d", inspection.Messages)
	}
	if inspection.MessagesWithSourceID != 3 || inspection.MessagesWithoutSourceID != 1 {
		testContext.Fatalf(
			"unexpected source-ID presence counts: with=%d without=%d",
			inspection.MessagesWithSourceID,
			inspection.MessagesWithoutSourceID,
		)
	}
	if inspection.UniqueSourceMessageIDs != 2 {
		testContext.Fatalf("expected 2 unique source IDs, received %d", inspection.UniqueSourceMessageIDs)
	}
	if inspection.RepeatedSourceMessageIDs != 1 || inspection.RepeatedSourceMessageOccurrences != 1 {
		testContext.Fatalf(
			"unexpected repeated source-ID counts: ids=%d occurrences=%d",
			inspection.RepeatedSourceMessageIDs,
			inspection.RepeatedSourceMessageOccurrences,
		)
	}
}
