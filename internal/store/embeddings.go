package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
)

func (store *Store) GetOrCreateEmbeddingConfig(contextValue context.Context, config domain.EmbeddingConfig) (domain.EmbeddingConfig, error) {
	config.CreatedAtMillis = time.Now().UTC().UnixMilli()
	_, insertError := store.database.ExecContext(
		contextValue,
		`INSERT INTO embedding_configs(
	            provider, model, dimensions, base_url, input_prefix, vector_path,
	            preprocessing_version, context_version, status, created_at_ms
	         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'building', ?)
	         ON CONFLICT(provider, model, dimensions, base_url, preprocessing_version, context_version) DO NOTHING`,
		config.Provider,
		config.Model,
		config.Dimensions,
		config.BaseURL,
		config.InputPrefix,
		config.VectorPath,
		config.PreprocessingVersion,
		config.ContextVersion,
		config.CreatedAtMillis,
	)
	if insertError != nil {
		return config, fmt.Errorf("create embedding configuration: %w", insertError)
	}

	loadedConfig, loadError := store.loadEmbeddingConfigByIdentity(contextValue, config)
	if loadError != nil {
		return config, loadError
	}
	return loadedConfig, nil
}

func (store *Store) loadEmbeddingConfigByIdentity(contextValue context.Context, identity domain.EmbeddingConfig) (domain.EmbeddingConfig, error) {
	var config domain.EmbeddingConfig
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT config_id, provider, model, dimensions, base_url, input_prefix, vector_path,
	                preprocessing_version, context_version, status, created_at_ms, completed_at_ms
         FROM embedding_configs
         WHERE provider = ? AND model = ? AND dimensions = ? AND base_url = ?
           AND preprocessing_version = ? AND context_version = ?`,
		identity.Provider,
		identity.Model,
		identity.Dimensions,
		identity.BaseURL,
		identity.PreprocessingVersion,
		identity.ContextVersion,
	).Scan(
		&config.ID,
		&config.Provider,
		&config.Model,
		&config.Dimensions,
		&config.BaseURL,
		&config.InputPrefix,
		&config.VectorPath,
		&config.PreprocessingVersion,
		&config.ContextVersion,
		&config.Status,
		&config.CreatedAtMillis,
		completedAtMillisScanner(&config.CompletedAtMillis),
	)
	if queryError != nil {
		return config, fmt.Errorf("load embedding configuration: %w", queryError)
	}
	return config, nil
}

func (store *Store) LatestReadyEmbeddingConfig(contextValue context.Context) (domain.EmbeddingConfig, error) {
	var config domain.EmbeddingConfig
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT config_id, provider, model, dimensions, base_url, input_prefix, vector_path,
	                preprocessing_version, context_version, status, created_at_ms, completed_at_ms
	         FROM embedding_configs
	         WHERE status = 'ready'
	         ORDER BY COALESCE(completed_at_ms, created_at_ms) DESC, config_id DESC
	         LIMIT 1`,
	).Scan(
		&config.ID,
		&config.Provider,
		&config.Model,
		&config.Dimensions,
		&config.BaseURL,
		&config.InputPrefix,
		&config.VectorPath,
		&config.PreprocessingVersion,
		&config.ContextVersion,
		&config.Status,
		&config.CreatedAtMillis,
		completedAtMillisScanner(&config.CompletedAtMillis),
	)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return config, fmt.Errorf("no ready embedding configuration exists; finish an embed run or select a building configuration explicitly")
		}
		return config, fmt.Errorf("load latest ready embedding configuration: %w", queryError)
	}
	return config, nil
}

func (store *Store) EmbeddingConfigByID(contextValue context.Context, configID int64) (domain.EmbeddingConfig, error) {
	var config domain.EmbeddingConfig
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT config_id, provider, model, dimensions, base_url, input_prefix, vector_path,
		        preprocessing_version, context_version, status, created_at_ms, completed_at_ms
		 FROM embedding_configs
		 WHERE config_id = ?`,
		configID,
	).Scan(
		&config.ID,
		&config.Provider,
		&config.Model,
		&config.Dimensions,
		&config.BaseURL,
		&config.InputPrefix,
		&config.VectorPath,
		&config.PreprocessingVersion,
		&config.ContextVersion,
		&config.Status,
		&config.CreatedAtMillis,
		completedAtMillisScanner(&config.CompletedAtMillis),
	)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return config, fmt.Errorf("embedding configuration %d does not exist", configID)
		}
		return config, fmt.Errorf("load embedding configuration %d: %w", configID, queryError)
	}
	return config, nil
}

func (store *Store) ListEmbeddingConfigSummaries(contextValue context.Context) ([]domain.EmbeddingConfigSummary, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT config.config_id, config.provider, config.model, config.dimensions,
		        config.base_url, config.input_prefix, config.vector_path,
		        config.preprocessing_version, config.context_version, config.status,
		        config.created_at_ms, config.completed_at_ms,
		        COUNT(embedding.message_id),
		        (SELECT COUNT(*) FROM messages WHERE role = 'user' AND normalized_text <> '')
		 FROM embedding_configs config
		 LEFT JOIN embeddings embedding ON embedding.config_id = config.config_id
		 GROUP BY config.config_id
		 ORDER BY config.config_id`,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list embedding configurations: %w", queryError)
	}
	defer rows.Close()

	summaries := make([]domain.EmbeddingConfigSummary, 0)
	for rows.Next() {
		var summary domain.EmbeddingConfigSummary
		if scanError := rows.Scan(
			&summary.Config.ID,
			&summary.Config.Provider,
			&summary.Config.Model,
			&summary.Config.Dimensions,
			&summary.Config.BaseURL,
			&summary.Config.InputPrefix,
			&summary.Config.VectorPath,
			&summary.Config.PreprocessingVersion,
			&summary.Config.ContextVersion,
			&summary.Config.Status,
			&summary.Config.CreatedAtMillis,
			completedAtMillisScanner(&summary.Config.CompletedAtMillis),
			&summary.EmbeddingCount,
			&summary.EligibleCount,
		); scanError != nil {
			return nil, fmt.Errorf("read embedding configuration summary: %w", scanError)
		}
		summaries = append(summaries, summary)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate embedding configuration summaries: %w", rowsError)
	}
	return summaries, nil
}

func (store *Store) MarkEmbeddingConfigBuilding(contextValue context.Context, configID int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE embedding_configs SET status = 'building', completed_at_ms = NULL WHERE config_id = ?`,
		configID,
	)
	if executeError != nil {
		return fmt.Errorf("mark embedding configuration %d building: %w", configID, executeError)
	}
	return nil
}

func (store *Store) MarkEmbeddingConfigReady(contextValue context.Context, configID int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE embedding_configs SET status = 'ready', completed_at_ms = ? WHERE config_id = ?`,
		time.Now().UTC().UnixMilli(),
		configID,
	)
	if executeError != nil {
		return fmt.Errorf("mark embedding configuration %d ready: %w", configID, executeError)
	}
	return nil
}

func (store *Store) ResolveVectorPath(config domain.EmbeddingConfig) string {
	if filepath.IsAbs(config.VectorPath) {
		return config.VectorPath
	}
	return filepath.Join(store.DatabaseDirectory(), config.VectorPath)
}

func (store *Store) MaximumVectorRow(contextValue context.Context, configID int64) (int64, error) {
	var maximumRow sql.NullInt64
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT MAX(vector_row) FROM embeddings WHERE config_id = ?`,
		configID,
	).Scan(&maximumRow)
	if queryError != nil {
		return -1, fmt.Errorf("read maximum vector row: %w", queryError)
	}
	if !maximumRow.Valid {
		return -1, nil
	}
	return maximumRow.Int64, nil
}

func (store *Store) SaveEmbeddingRecords(contextValue context.Context, configID int64, records []domain.EmbeddingRecord) error {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin embedding transaction: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	for _, record := range records {
		_, executeError := transaction.ExecContext(
			contextValue,
			`INSERT INTO embeddings(
                message_id, config_id, vector_row, dimensions, search_text_hash, created_at_ms
             ) VALUES (?, ?, ?, ?, ?, ?)
             ON CONFLICT(message_id, config_id) DO UPDATE SET
                vector_row = excluded.vector_row,
                dimensions = excluded.dimensions,
                search_text_hash = excluded.search_text_hash,
                created_at_ms = excluded.created_at_ms`,
			record.MessageID,
			configID,
			record.VectorRow,
			record.Dimensions,
			record.SearchTextHash,
			record.CreatedAtMillis,
		)
		if executeError != nil {
			return fmt.Errorf("save embedding for message %s: %w", record.MessageID, executeError)
		}
	}

	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit embedding records: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) ListEmbeddingCandidates(contextValue context.Context, configID int64, batchSize int, refreshStale bool, afterMessageID string) ([]domain.EmbeddingCandidate, error) {
	comparisonClause := `embedding.message_id IS NULL`
	if refreshStale {
		comparisonClause = `1 = 1`
	}
	query := fmt.Sprintf(`
SELECT
    message.message_id,
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
    COALESCE(embedding.search_text_hash, '')
FROM messages message
JOIN conversations conversation
  ON conversation.conversation_id = message.conversation_id
LEFT JOIN embeddings embedding
  ON embedding.message_id = message.message_id
 AND embedding.config_id = ?
WHERE message.role = 'user'
	  AND message.normalized_text <> ''
	  AND message.message_id > ?
	  AND %s
ORDER BY message.message_id
	LIMIT ?`, comparisonClause)

	rows, queryError := store.database.QueryContext(contextValue, query, configID, afterMessageID, batchSize)
	if queryError != nil {
		return nil, fmt.Errorf("list messages requiring embeddings: %w", queryError)
	}
	defer rows.Close()

	candidates := make([]domain.EmbeddingCandidate, 0, batchSize)
	for rows.Next() {
		var candidate domain.EmbeddingCandidate
		var createdAt sql.NullInt64
		var archived sql.NullInt64
		if scanError := rows.Scan(
			&candidate.MessageID,
			&candidate.ConversationID,
			&candidate.ConversationTitle,
			&candidate.OriginalText,
			&candidate.NormalizedText,
			&createdAt,
			&archived,
			&candidate.ParentText,
			&candidate.FollowingText,
			&candidate.ExistingSearchHash,
		); scanError != nil {
			return nil, fmt.Errorf("read embedding candidate: %w", scanError)
		}
		candidate.CreatedAtMillis = nullableInt64Pointer(createdAt)
		candidate.IsArchived = nullableBoolPointer(archived)
		candidates = append(candidates, candidate)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate embedding candidates: %w", rowsError)
	}
	return candidates, nil
}

func (store *Store) CountEmbeddings(contextValue context.Context, configID int64) (int64, error) {
	var embeddingCount int64
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT COUNT(*) FROM embeddings WHERE config_id = ?`,
		configID,
	).Scan(&embeddingCount)
	if queryError != nil {
		return 0, fmt.Errorf("count embeddings: %w", queryError)
	}
	return embeddingCount, nil
}

func (store *Store) SavePrototypeVector(contextValue context.Context, configID int64, intentName string, label string, exampleText string, vector []float32) error {
	exampleHash := normalize.Hash(intentName, label, exampleText)
	vectorBytes := make([]byte, len(vector)*4)
	for vectorIndex, vectorValue := range vector {
		binary.LittleEndian.PutUint32(vectorBytes[vectorIndex*4:], math.Float32bits(vectorValue))
	}
	_, executeError := store.database.ExecContext(
		contextValue,
		`INSERT INTO intent_prototypes(
            config_id, intent_name, example_hash, label, example_text,
            vector_blob, dimensions, created_at_ms
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(config_id, intent_name, example_hash) DO UPDATE SET
            label = excluded.label,
            example_text = excluded.example_text,
            vector_blob = excluded.vector_blob,
            dimensions = excluded.dimensions,
            created_at_ms = excluded.created_at_ms`,
		configID,
		intentName,
		exampleHash,
		label,
		exampleText,
		vectorBytes,
		len(vector),
		time.Now().UTC().UnixMilli(),
	)
	if executeError != nil {
		return fmt.Errorf("save intent prototype: %w", executeError)
	}
	return nil
}

func (store *Store) LoadPrototypeVector(contextValue context.Context, configID int64, intentName string, label string, exampleText string) ([]float32, bool, error) {
	exampleHash := normalize.Hash(intentName, label, exampleText)
	var vectorBytes []byte
	var dimensions int
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT vector_blob, dimensions
         FROM intent_prototypes
         WHERE config_id = ? AND intent_name = ? AND example_hash = ?`,
		configID,
		intentName,
		exampleHash,
	).Scan(&vectorBytes, &dimensions)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load intent prototype: %w", queryError)
	}
	if len(vectorBytes) != dimensions*4 {
		return nil, false, fmt.Errorf("stored intent prototype has invalid byte length")
	}
	vector := make([]float32, dimensions)
	for vectorIndex := 0; vectorIndex < dimensions; vectorIndex++ {
		vector[vectorIndex] = math.Float32frombits(binary.LittleEndian.Uint32(vectorBytes[vectorIndex*4:]))
	}
	return vector, true, nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copiedValue := value.Int64
	return &copiedValue
}

func nullableBoolPointer(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	copiedValue := value.Int64 != 0
	return &copiedValue
}

func completedAtMillisScanner(destination **int64) any {
	return &nullableInt64Destination{destination: destination}
}

type nullableInt64Destination struct {
	destination **int64
}

func (destination *nullableInt64Destination) Scan(value any) error {
	if value == nil {
		*destination.destination = nil
		return nil
	}
	integerValue, valid := value.(int64)
	if !valid {
		return fmt.Errorf("expected int64 database value, received %T", value)
	}
	copiedValue := integerValue
	*destination.destination = &copiedValue
	return nil
}
