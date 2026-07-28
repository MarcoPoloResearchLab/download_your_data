package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

func TestImporterPreservesArchivedConversationsAndBranches(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	sourcePath := filepath.Join("..", "..", "testdata", "synthetic-openai-export.zip")
	importer := Importer{Store: openedStore}
	result, importError := importer.Import(context.Background(), sourcePath, false)
	if importError != nil {
		testContext.Fatalf("import fixture: %v", importError)
	}
	if result.ConversationsSeen != 2 {
		testContext.Fatalf("expected 2 conversations, received %d", result.ConversationsSeen)
	}
	if result.MessagesSeen != 10 {
		testContext.Fatalf("expected all 10 branch messages, received %d", result.MessagesSeen)
	}

	statistics, statsError := openedStore.Stats(context.Background())
	if statsError != nil {
		testContext.Fatalf("read statistics: %v", statsError)
	}
	if statistics.ArchivedConversations != 1 {
		testContext.Fatalf("expected one archived conversation, received %d", statistics.ArchivedConversations)
	}
	if statistics.UserMessages != 5 {
		testContext.Fatalf("expected all branch user messages, received %d", statistics.UserMessages)
	}
}

func TestImporterPreservesRepeatedSourceMessageIDsAsDistinctOccurrences(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	exportPath := filepath.Join(testContext.TempDir(), "conversations.json")
	exportJSON := `[
  {
    "id": "conversation-a",
    "title": "First",
    "create_time": 1700000000,
    "mapping": {
      "node-a-user": {
        "id": "node-a-user",
        "parent": null,
        "children": ["node-a-assistant"],
        "message": {
          "id": "reused-user-message-id",
          "author": {"role": "user"},
          "create_time": 1700000001,
          "content": {"content_type": "text", "parts": ["define berth"]}
        }
      },
      "node-a-assistant": {
        "id": "node-a-assistant",
        "parent": "node-a-user",
        "children": [],
        "message": {
          "id": "reused-assistant-message-id",
          "author": {"role": "assistant"},
          "create_time": 1700000002,
          "content": {"content_type": "text", "parts": ["A berth is a bed or assigned place."]}
        }
      }
    }
  },
  {
    "id": "conversation-b",
    "title": "Second",
    "create_time": 1700000100,
    "mapping": {
      "node-b-user": {
        "id": "node-b-user",
        "parent": null,
        "children": ["node-b-assistant"],
        "message": {
          "id": "reused-user-message-id",
          "author": {"role": "user"},
          "create_time": 1700000101,
          "content": {"content_type": "text", "parts": ["what does berth mean"]}
        }
      },
      "node-b-assistant": {
        "id": "node-b-assistant",
        "parent": "node-b-user",
        "children": [],
        "message": {
          "id": "reused-assistant-message-id",
          "author": {"role": "assistant"},
          "create_time": 1700000102,
          "content": {"content_type": "text", "parts": ["It can mean a sleeping place or docking space."]}
        }
      }
    }
  }
]`
	if writeError := os.WriteFile(exportPath, []byte(exportJSON), 0o600); writeError != nil {
		testContext.Fatalf("write repeated-ID export: %v", writeError)
	}

	importer := Importer{Store: openedStore}
	result, importError := importer.Import(context.Background(), exportPath, false)
	if importError != nil {
		testContext.Fatalf("import repeated-ID fixture: %v", importError)
	}
	if result.MessagesSeen != 4 {
		testContext.Fatalf("expected 4 source occurrences, received %d", result.MessagesSeen)
	}

	statistics, statsError := openedStore.Stats(context.Background())
	if statsError != nil {
		testContext.Fatalf("read statistics: %v", statsError)
	}
	if statistics.Messages != 4 {
		testContext.Fatalf("expected all 4 occurrences to remain stored, received %d", statistics.Messages)
	}
	if statistics.UserMessages != 2 || statistics.AssistantMessages != 2 {
		testContext.Fatalf(
			"expected 2 user and 2 assistant occurrences, received %d user and %d assistant",
			statistics.UserMessages,
			statistics.AssistantMessages,
		)
	}
	if statistics.SourceMessageIDs != 4 || statistics.UniqueSourceMessageIDs != 2 || statistics.RepeatedSourceMessages != 2 {
		testContext.Fatalf(
			"unexpected source-message identity statistics: present=%d unique=%d repeated_occurrences=%d",
			statistics.SourceMessageIDs,
			statistics.UniqueSourceMessageIDs,
			statistics.RepeatedSourceMessages,
		)
	}

	var distinctOccurrenceIDs int64
	queryError := openedStore.Database().QueryRowContext(
		context.Background(),
		`SELECT COUNT(DISTINCT message_id) FROM messages`,
	).Scan(&distinctOccurrenceIDs)
	if queryError != nil {
		testContext.Fatalf("count occurrence IDs: %v", queryError)
	}
	if distinctOccurrenceIDs != 4 {
		testContext.Fatalf("expected 4 distinct occurrence IDs, received %d", distinctOccurrenceIDs)
	}
}

func TestEmbeddingContextExcludesAssistantThoughtRecords(testContext *testing.T) {
	databasePath := filepath.Join(testContext.TempDir(), "archive.db")
	openedStore, openError := store.Open(databasePath)
	if openError != nil {
		testContext.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()

	exportPath := filepath.Join(testContext.TempDir(), "conversations.json")
	exportJSON := `[
  {
    "id": "context-conversation",
    "title": "Context filter",
    "create_time": 1700000000,
    "mapping": {
      "parent-thought": {
        "id": "parent-thought",
        "parent": null,
        "children": ["user-node"],
        "message": {
          "id": "thought-before",
          "author": {"role": "assistant"},
          "create_time": 1700000001,
          "content": {"content_type": "thoughts", "parts": ["internal reasoning before the question"]}
        }
      },
      "user-node": {
        "id": "user-node",
        "parent": "parent-thought",
        "children": ["child-thought", "child-answer"],
        "message": {
          "id": "context-user",
          "author": {"role": "user"},
          "create_time": 1700000002,
          "content": {"content_type": "text", "parts": ["what does that mean?"]}
        }
      },
      "child-thought": {
        "id": "child-thought",
        "parent": "user-node",
        "children": [],
        "message": {
          "id": "thought-after",
          "author": {"role": "assistant"},
          "create_time": 1700000003,
          "content": {"content_type": "thoughts", "parts": ["internal reasoning after the question"]}
        }
      },
      "child-answer": {
        "id": "child-answer",
        "parent": "user-node",
        "children": [],
        "message": {
          "id": "visible-answer",
          "author": {"role": "assistant"},
          "create_time": 1700000004,
          "content": {"content_type": "text", "parts": ["This is the visible answer."]}
        }
      }
    }
  }
]`
	if writeError := os.WriteFile(exportPath, []byte(exportJSON), 0o600); writeError != nil {
		testContext.Fatalf("write context export: %v", writeError)
	}

	importer := Importer{Store: openedStore}
	if _, importError := importer.Import(context.Background(), exportPath, false); importError != nil {
		testContext.Fatalf("import context fixture: %v", importError)
	}

	candidates, candidateError := openedStore.ListEmbeddingCandidates(context.Background(), 0, 10, false, "")
	if candidateError != nil {
		testContext.Fatalf("list embedding candidates: %v", candidateError)
	}
	if len(candidates) != 1 {
		testContext.Fatalf("expected 1 user candidate, received %d", len(candidates))
	}
	if candidates[0].ParentText != "" {
		testContext.Fatalf("expected thoughts parent to be excluded, received %q", candidates[0].ParentText)
	}
	if candidates[0].FollowingText != "This is the visible answer." {
		testContext.Fatalf("expected visible assistant answer, received %q", candidates[0].FollowingText)
	}
}
