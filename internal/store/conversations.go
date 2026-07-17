package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
)

func (store *Store) ImportConversation(contextValue context.Context, importID int64, conversation domain.Conversation) (int64, int64, error) {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return 0, 0, fmt.Errorf("begin conversation transaction: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	_, conversationError := transaction.ExecContext(
		contextValue,
		`INSERT INTO conversations(
            conversation_id, title, created_at_ms, updated_at_ms, is_archived,
            current_node_id, source_import_id, source_filename, raw_metadata_json
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(conversation_id) DO UPDATE SET
            title = excluded.title,
            created_at_ms = COALESCE(excluded.created_at_ms, conversations.created_at_ms),
            updated_at_ms = COALESCE(excluded.updated_at_ms, conversations.updated_at_ms),
            is_archived = COALESCE(excluded.is_archived, conversations.is_archived),
            current_node_id = excluded.current_node_id,
            source_import_id = excluded.source_import_id,
            source_filename = excluded.source_filename,
            raw_metadata_json = excluded.raw_metadata_json`,
		conversation.ID,
		conversation.Title,
		timeToNullableMillis(conversation.CreatedAt),
		timeToNullableMillis(conversation.UpdatedAt),
		boolToNullableInteger(conversation.IsArchived),
		conversation.CurrentNodeID,
		importID,
		conversation.SourceFile,
		nullableString(conversation.RawMetadata),
	)
	if conversationError != nil {
		return 0, 0, fmt.Errorf("upsert conversation %s: %w", conversation.ID, conversationError)
	}

	var messageCount int64
	var warningCount int64
	for _, message := range conversation.Messages {
		if message.ExtractionWarning != "" {
			warningCount++
		}
		if messageError := upsertMessage(contextValue, transaction, message); messageError != nil {
			return 0, 0, messageError
		}
		messageCount++
	}

	for _, edge := range conversation.Edges {
		_, edgeError := transaction.ExecContext(
			contextValue,
			`INSERT INTO message_edges(conversation_id, parent_node_id, child_node_id)
             VALUES (?, ?, ?)
             ON CONFLICT(conversation_id, parent_node_id, child_node_id) DO NOTHING`,
			edge.ConversationID,
			edge.ParentNodeID,
			edge.ChildNodeID,
		)
		if edgeError != nil {
			return 0, 0, fmt.Errorf("insert message edge: %w", edgeError)
		}
	}

	if commitError := transaction.Commit(); commitError != nil {
		return 0, 0, fmt.Errorf("commit conversation transaction: %w", commitError)
	}
	rollbackRequired = false
	return messageCount, warningCount, nil
}

func upsertMessage(contextValue context.Context, transaction *sql.Tx, message domain.Message) error {
	_, messageError := transaction.ExecContext(
		contextValue,
		`INSERT INTO messages(
            message_id, source_message_id, conversation_id, parent_node_id, role, created_at_ms,
            content_type, original_text, normalized_text, content_hash,
            source_filename, source_node_id, extraction_warning, raw_metadata_json
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(message_id) DO UPDATE SET
            source_message_id = excluded.source_message_id,
            conversation_id = excluded.conversation_id,
            parent_node_id = excluded.parent_node_id,
            role = excluded.role,
            created_at_ms = COALESCE(excluded.created_at_ms, messages.created_at_ms),
            content_type = excluded.content_type,
            original_text = excluded.original_text,
            normalized_text = excluded.normalized_text,
            content_hash = excluded.content_hash,
            source_filename = excluded.source_filename,
            source_node_id = excluded.source_node_id,
            extraction_warning = excluded.extraction_warning,
            raw_metadata_json = excluded.raw_metadata_json`,
		message.ID,
		nullableString(message.SourceMessageID),
		message.ConversationID,
		nullableString(message.ParentNodeID),
		message.Role,
		timeToNullableMillis(message.CreatedAt),
		nullableString(message.ContentType),
		message.OriginalText,
		message.NormalizedText,
		message.ContentHash,
		message.SourceFile,
		message.SourceNodeID,
		nullableString(message.ExtractionWarning),
		nullableString(message.RawMetadata),
	)
	if messageError != nil {
		return fmt.Errorf("upsert message %s: %w", message.ID, messageError)
	}

	return nil
}

func timeToNullableMillis(timeValue *time.Time) any {
	if timeValue == nil {
		return nil
	}
	return timeValue.UTC().UnixMilli()
}

func boolToNullableInteger(boolValue *bool) any {
	if boolValue == nil {
		return nil
	}
	if *boolValue {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
