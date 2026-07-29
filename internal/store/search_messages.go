package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
)

func (store *Store) ListSearchMessages(
	contextValue context.Context,
	configID int64,
	sinceMillis int64,
	untilMillis int64,
	includeArchived bool,
) ([]domain.SearchMessage, error) {
	archivedFilter := `1 = 1`
	if !includeArchived {
		archivedFilter = `COALESCE(conversation.is_archived, 0) = 0`
	}

	query := fmt.Sprintf(`
SELECT
    message.message_id,
    COALESCE(message.source_message_id, ''),
    message.conversation_id,
    conversation.title,
    message.original_text,
    message.normalized_text,
    message.created_at_ms,
    conversation.is_archived,
    COALESCE((
        SELECT parent_message.original_text
        FROM messages parent_message
        WHERE parent_message.conversation_id = message.conversation_id
          AND parent_message.source_node_id = message.parent_node_id
          AND parent_message.role = 'assistant'
          AND parent_message.content_type IN ('text', 'multimodal_text')
          AND parent_message.normalized_text <> ''
        LIMIT 1
    ), ''),
    COALESCE((
        SELECT child_message.original_text
        FROM message_edges edge
        JOIN messages child_message
          ON child_message.conversation_id = edge.conversation_id
         AND child_message.source_node_id = edge.child_node_id
        WHERE edge.conversation_id = message.conversation_id
          AND edge.parent_node_id = message.source_node_id
          AND child_message.role = 'assistant'
          AND child_message.content_type IN ('text', 'multimodal_text')
          AND child_message.normalized_text <> ''
        ORDER BY child_message.created_at_ms ASC
        LIMIT 1
    ), ''),
    embedding.vector_row
FROM messages message
JOIN conversations conversation
  ON conversation.conversation_id = message.conversation_id
LEFT JOIN embeddings embedding
  ON embedding.message_id = message.message_id
 AND embedding.config_id = ?
WHERE message.role = 'user'
  AND message.normalized_text <> ''
  AND message.created_at_ms >= ?
  AND message.created_at_ms < ?
  AND %s
ORDER BY message.created_at_ms ASC, message.message_id ASC`, archivedFilter)

	rows, queryError := store.database.QueryContext(contextValue, query, configID, sinceMillis, untilMillis)
	if queryError != nil {
		return nil, fmt.Errorf("list messages for definition analysis: %w", queryError)
	}
	defer rows.Close()

	messages := make([]domain.SearchMessage, 0)
	for rows.Next() {
		var message domain.SearchMessage
		var createdAt sql.NullInt64
		var archived sql.NullInt64
		var vectorRow sql.NullInt64
		if scanError := rows.Scan(
			&message.MessageID,
			&message.SourceMessageID,
			&message.ConversationID,
			&message.ConversationTitle,
			&message.OriginalText,
			&message.NormalizedText,
			&createdAt,
			&archived,
			&message.ParentText,
			&message.FollowingText,
			&vectorRow,
		); scanError != nil {
			return nil, fmt.Errorf("read definition-analysis message: %w", scanError)
		}
		message.CreatedAtMillis = nullableInt64Pointer(createdAt)
		message.IsArchived = nullableBoolPointer(archived)
		message.VectorRow = nullableInt64Pointer(vectorRow)
		messages = append(messages, message)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate definition-analysis messages: %w", rowsError)
	}
	return messages, nil
}
