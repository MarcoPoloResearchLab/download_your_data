package store

import (
	"context"
	"fmt"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
)

func (store *Store) Stats(contextValue context.Context) (domain.StoreStats, error) {
	statistics := domain.StoreStats{}
	queries := []struct {
		query       string
		destination *int64
	}{
		{`SELECT COUNT(*) FROM imports WHERE status = 'completed'`, &statistics.Imports},
		{`SELECT COALESCE((SELECT messages_seen FROM imports WHERE status = 'completed' ORDER BY import_id DESC LIMIT 1), 0)`, &statistics.LatestImportMessages},
		{`SELECT COUNT(*) FROM conversations`, &statistics.Conversations},
		{`SELECT COUNT(*) FROM conversations WHERE is_archived = 1`, &statistics.ArchivedConversations},
		{`SELECT COUNT(*) FROM messages`, &statistics.Messages},
		{`SELECT COUNT(*) FROM messages WHERE role = 'user'`, &statistics.UserMessages},
		{`SELECT COUNT(*) FROM messages WHERE role = 'assistant'`, &statistics.AssistantMessages},
		{`SELECT COUNT(*) FROM messages WHERE source_message_id IS NOT NULL AND source_message_id <> ''`, &statistics.SourceMessageIDs},
		{`SELECT COUNT(DISTINCT source_message_id) FROM messages WHERE source_message_id IS NOT NULL AND source_message_id <> ''`, &statistics.UniqueSourceMessageIDs},
		{`SELECT COUNT(*) FROM messages WHERE source_message_id IS NULL OR source_message_id = ''`, &statistics.MessagesWithoutSourceID},
		{`SELECT COUNT(*) FROM embedding_configs`, &statistics.EmbeddingConfigurations},
		{`SELECT COUNT(*) FROM embeddings`, &statistics.Embeddings},
	}

	for _, countQuery := range queries {
		if queryError := store.database.QueryRowContext(contextValue, countQuery.query).Scan(countQuery.destination); queryError != nil {
			return statistics, fmt.Errorf("read database statistics: %w", queryError)
		}
	}
	statistics.RepeatedSourceMessages = statistics.SourceMessageIDs - statistics.UniqueSourceMessageIDs
	return statistics, nil
}
