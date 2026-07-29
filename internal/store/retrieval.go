package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
)

func (store *Store) ResolveSearchVectorFile(config domain.SearchIndexConfig) (privatepath.File, error) {
	return store.resolveDatabaseRelativeFile(config.VectorPath)
}

func (store *Store) ListRetrievalSourceNodes(contextValue context.Context) ([]domain.RetrievalSourceNode, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT message.message_id, message.conversation_id, conversation.title,
		        message.source_node_id, COALESCE(message.parent_node_id, ''),
		        message.role, COALESCE(message.content_type, ''),
		        message.original_text, message.normalized_text,
		        message.created_at_ms, conversation.is_archived
		 FROM messages message
		 JOIN conversations conversation ON conversation.conversation_id = message.conversation_id
		 ORDER BY message.conversation_id, message.message_id`,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list retrieval source nodes: %w", queryError)
	}
	defer rows.Close()

	nodes := make([]domain.RetrievalSourceNode, 0)
	for rows.Next() {
		var node domain.RetrievalSourceNode
		var createdAt sql.NullInt64
		var archived sql.NullInt64
		if scanError := rows.Scan(
			&node.MessageID,
			&node.ConversationID,
			&node.ConversationTitle,
			&node.SourceNodeID,
			&node.ParentNodeID,
			&node.Role,
			&node.ContentType,
			&node.OriginalText,
			&node.NormalizedText,
			&createdAt,
			&archived,
		); scanError != nil {
			return nil, fmt.Errorf("read retrieval source node: %w", scanError)
		}
		node.CreatedAtMillis = nullableInt64Pointer(createdAt)
		node.IsArchived = nullableBoolPointer(archived)
		nodes = append(nodes, node)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate retrieval source nodes: %w", rowsError)
	}
	return nodes, nil
}

func (store *Store) ListMessageEdges(contextValue context.Context) ([]domain.MessageEdge, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT conversation_id, parent_node_id, child_node_id
		 FROM message_edges
		 ORDER BY conversation_id, child_node_id, parent_node_id`,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list message edges: %w", queryError)
	}
	defer rows.Close()
	edges := make([]domain.MessageEdge, 0)
	for rows.Next() {
		var edge domain.MessageEdge
		if scanError := rows.Scan(&edge.ConversationID, &edge.ParentNodeID, &edge.ChildNodeID); scanError != nil {
			return nil, fmt.Errorf("read message edge: %w", scanError)
		}
		edges = append(edges, edge)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate message edges: %w", rowsError)
	}
	return edges, nil
}

func (store *Store) GetOrCreateSearchIndex(contextValue context.Context, config domain.SearchIndexConfig) (domain.SearchIndexConfig, error) {
	if _, pathError := store.resolveDatabaseRelativeFile(config.VectorPath); pathError != nil {
		return config, pathError
	}
	config.CreatedAtMillis = time.Now().UTC().UnixMilli()
	_, insertError := store.database.ExecContext(
		contextValue,
		`INSERT INTO search_indexes(
			name, provider, model, dimensions, base_url, document_prefix, query_prefix,
			vector_path, builder_version, corpus_policy, status, created_at_ms
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'building', ?)
		 ON CONFLICT(name) DO NOTHING`,
		config.Name,
		config.Provider,
		config.Model,
		config.Dimensions,
		config.BaseURL,
		config.DocumentPrefix,
		config.QueryPrefix,
		config.VectorPath,
		config.BuilderVersion,
		config.CorpusPolicy,
		config.CreatedAtMillis,
	)
	if insertError != nil {
		return config, fmt.Errorf("create search index: %w", insertError)
	}
	loaded, loadError := store.SearchIndexByName(contextValue, config.Name)
	if loadError != nil {
		return config, loadError
	}
	if loaded.Provider != config.Provider || loaded.Model != config.Model ||
		loaded.Dimensions != config.Dimensions || loaded.BaseURL != config.BaseURL ||
		loaded.DocumentPrefix != config.DocumentPrefix || loaded.QueryPrefix != config.QueryPrefix ||
		loaded.VectorPath != config.VectorPath ||
		loaded.BuilderVersion != config.BuilderVersion || loaded.CorpusPolicy != config.CorpusPolicy {
		return loaded, fmt.Errorf(
			"search index %q already exists with a different identity; rerun with --rebuild to replace it",
			config.Name,
		)
	}
	return loaded, nil
}

// DeleteSearchIndexByName validates the stored private vector file before
// removing one index's relational and FTS state. The delete cascades to
// search_documents and query_embedding_cache.
func (store *Store) DeleteSearchIndexByName(
	contextValue context.Context,
	name string,
) (privatepath.File, bool, error) {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return privatepath.File{}, false, fmt.Errorf("begin search index deletion: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	var indexID int64
	var vectorPath string
	queryError := transaction.QueryRowContext(
		contextValue,
		`SELECT search_index_id, vector_path FROM search_indexes WHERE name=?`,
		name,
	).Scan(&indexID, &vectorPath)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			if rollbackError := transaction.Rollback(); rollbackError != nil {
				return privatepath.File{}, false, fmt.Errorf("rollback absent search index deletion: %w", rollbackError)
			}
			rollbackRequired = false
			return privatepath.File{}, false, nil
		}
		return privatepath.File{}, false, fmt.Errorf("locate search index %q for deletion: %w", name, queryError)
	}
	vectorFile, pathError := store.resolveDatabaseRelativeFile(vectorPath)
	if pathError != nil {
		return privatepath.File{}, false, pathError
	}
	if _, deleteError := transaction.ExecContext(
		contextValue,
		`DELETE FROM search_documents_fts WHERE search_index_id=?`,
		indexID,
	); deleteError != nil {
		return privatepath.File{}, false, fmt.Errorf("delete search index %q FTS documents: %w", name, deleteError)
	}
	if _, deleteError := transaction.ExecContext(
		contextValue,
		`DELETE FROM search_indexes WHERE search_index_id=?`,
		indexID,
	); deleteError != nil {
		return privatepath.File{}, false, fmt.Errorf("delete search index %q: %w", name, deleteError)
	}
	if commitError := transaction.Commit(); commitError != nil {
		return privatepath.File{}, false, fmt.Errorf("commit search index %q deletion: %w", name, commitError)
	}
	rollbackRequired = false
	return vectorFile, true, nil
}

func (store *Store) SearchIndexByName(contextValue context.Context, name string) (domain.SearchIndexConfig, error) {
	return store.loadSearchIndex(
		contextValue,
		`WHERE name = ?`,
		[]any{name},
		fmt.Sprintf("search index %q does not exist", name),
	)
}

func (store *Store) SearchIndexByID(contextValue context.Context, indexID int64) (domain.SearchIndexConfig, error) {
	return store.loadSearchIndex(
		contextValue,
		`WHERE search_index_id = ?`,
		[]any{indexID},
		fmt.Sprintf("search index %d does not exist", indexID),
	)
}

func (store *Store) LatestReadySearchIndex(contextValue context.Context) (domain.SearchIndexConfig, error) {
	return store.loadSearchIndex(
		contextValue,
		`WHERE status = 'ready' ORDER BY COALESCE(completed_at_ms, created_at_ms) DESC, search_index_id DESC LIMIT 1`,
		nil,
		fmt.Sprintf(
			"no ready conversation search index exists; run %s index build first",
			product.CommandName,
		),
	)
}

func (store *Store) loadSearchIndex(contextValue context.Context, suffix string, arguments []any, notFoundMessage string) (domain.SearchIndexConfig, error) {
	var config domain.SearchIndexConfig
	query := `SELECT search_index_id, name, provider, model, dimensions, base_url,
	                 document_prefix, query_prefix, vector_path, builder_version,
	                 corpus_policy, status, created_at_ms, completed_at_ms
	          FROM search_indexes ` + suffix
	queryError := store.database.QueryRowContext(contextValue, query, arguments...).Scan(
		&config.ID,
		&config.Name,
		&config.Provider,
		&config.Model,
		&config.Dimensions,
		&config.BaseURL,
		&config.DocumentPrefix,
		&config.QueryPrefix,
		&config.VectorPath,
		&config.BuilderVersion,
		&config.CorpusPolicy,
		&config.Status,
		&config.CreatedAtMillis,
		completedAtMillisScanner(&config.CompletedAtMillis),
	)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return config, fmt.Errorf("%s", notFoundMessage)
		}
		return config, fmt.Errorf("load search index: %w", queryError)
	}
	return config, nil
}

func (store *Store) ListSearchIndexSummaries(contextValue context.Context) ([]domain.SearchIndexSummary, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT search_index.search_index_id, search_index.name, search_index.provider,
		        search_index.model, search_index.dimensions, search_index.base_url,
		        search_index.document_prefix, search_index.query_prefix, search_index.vector_path,
		        search_index.builder_version, search_index.corpus_policy, search_index.status,
		        search_index.created_at_ms, search_index.completed_at_ms,
		        COUNT(search_document.document_id),
		        (SELECT COUNT(*) FROM messages
		         WHERE role IN ('user', 'assistant')
		           AND content_type IN ('text', 'multimodal_text')
		           AND normalized_text <> ''),
		        COUNT(DISTINCT search_document.conversation_id),
		        (SELECT COUNT(DISTINCT conversation_id) FROM messages
		         WHERE role IN ('user', 'assistant')
		           AND content_type IN ('text', 'multimodal_text')
		           AND normalized_text <> '')
		 FROM search_indexes search_index
		 LEFT JOIN search_documents search_document
		   ON search_document.search_index_id = search_index.search_index_id
		 GROUP BY search_index.search_index_id
		 ORDER BY search_index.search_index_id`,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list search index summaries: %w", queryError)
	}
	defer rows.Close()
	summaries := make([]domain.SearchIndexSummary, 0)
	for rows.Next() {
		var summary domain.SearchIndexSummary
		if scanError := rows.Scan(
			&summary.Config.ID,
			&summary.Config.Name,
			&summary.Config.Provider,
			&summary.Config.Model,
			&summary.Config.Dimensions,
			&summary.Config.BaseURL,
			&summary.Config.DocumentPrefix,
			&summary.Config.QueryPrefix,
			&summary.Config.VectorPath,
			&summary.Config.BuilderVersion,
			&summary.Config.CorpusPolicy,
			&summary.Config.Status,
			&summary.Config.CreatedAtMillis,
			completedAtMillisScanner(&summary.Config.CompletedAtMillis),
			&summary.DocumentCount,
			&summary.EligibleCount,
			&summary.CoveredConversations,
			&summary.EligibleConversations,
		); scanError != nil {
			return nil, fmt.Errorf("read search index summary: %w", scanError)
		}
		summaries = append(summaries, summary)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate search index summaries: %w", rowsError)
	}
	return summaries, nil
}

func (store *Store) MarkSearchIndexBuilding(contextValue context.Context, indexID int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE search_indexes SET status='building', completed_at_ms=NULL WHERE search_index_id=?`,
		indexID,
	)
	if executeError != nil {
		return fmt.Errorf("mark search index %d building: %w", indexID, executeError)
	}
	return nil
}

func (store *Store) MarkSearchIndexReady(contextValue context.Context, indexID int64) error {
	_, executeError := store.database.ExecContext(
		contextValue,
		`UPDATE search_indexes SET status='ready', completed_at_ms=? WHERE search_index_id=?`,
		time.Now().UTC().UnixMilli(),
		indexID,
	)
	if executeError != nil {
		return fmt.Errorf("mark search index %d ready: %w", indexID, executeError)
	}
	return nil
}

func (store *Store) MaximumSearchVectorRow(contextValue context.Context, indexID int64) (int64, error) {
	var maximumRow sql.NullInt64
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT MAX(vector_row) FROM search_documents WHERE search_index_id=?`,
		indexID,
	).Scan(&maximumRow)
	if queryError != nil {
		return -1, fmt.Errorf("read maximum search vector row: %w", queryError)
	}
	if !maximumRow.Valid {
		return -1, nil
	}
	return maximumRow.Int64, nil
}

func (store *Store) ListSearchDocumentStates(contextValue context.Context, indexID int64) (map[string]domain.SearchDocumentState, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT document_id, text_hash, vector_row FROM search_documents WHERE search_index_id=?`,
		indexID,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list search document states: %w", queryError)
	}
	defer rows.Close()
	states := make(map[string]domain.SearchDocumentState)
	for rows.Next() {
		var documentID string
		var state domain.SearchDocumentState
		var vectorRow sql.NullInt64
		if scanError := rows.Scan(&documentID, &state.TextHash, &vectorRow); scanError != nil {
			return nil, fmt.Errorf("read search document state: %w", scanError)
		}
		state.VectorRow = nullableInt64Pointer(vectorRow)
		states[documentID] = state
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate search document states: %w", rowsError)
	}
	return states, nil
}

func (store *Store) SaveSearchDocuments(contextValue context.Context, indexID int64, documents []domain.SearchDocument) error {
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin search document transaction: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()

	for _, document := range documents {
		if document.VectorRow == nil {
			return fmt.Errorf("search document %s has no vector row", document.ID)
		}
		if _, deleteError := transaction.ExecContext(
			contextValue,
			`DELETE FROM search_documents_fts WHERE search_index_id=? AND document_id=?`,
			indexID,
			document.ID,
		); deleteError != nil {
			return fmt.Errorf("delete prior search FTS document %s: %w", document.ID, deleteError)
		}
		if _, executeError := transaction.ExecContext(
			contextValue,
			`INSERT INTO search_documents(
				search_index_id, document_id, conversation_id, anchor_message_id,
				conversation_title, role, content_type, source_text, document_text,
				text_hash, created_at_ms, is_archived, vector_row, dimensions, embedded_at_ms
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(search_index_id, document_id) DO UPDATE SET
				conversation_id=excluded.conversation_id,
				anchor_message_id=excluded.anchor_message_id,
				conversation_title=excluded.conversation_title,
				role=excluded.role,
				content_type=excluded.content_type,
				source_text=excluded.source_text,
				document_text=excluded.document_text,
				text_hash=excluded.text_hash,
				created_at_ms=excluded.created_at_ms,
				is_archived=excluded.is_archived,
				vector_row=excluded.vector_row,
				dimensions=excluded.dimensions,
				embedded_at_ms=excluded.embedded_at_ms`,
			indexID,
			document.ID,
			document.ConversationID,
			document.AnchorMessageID,
			document.ConversationTitle,
			document.Role,
			document.ContentType,
			document.SourceText,
			document.Text,
			document.TextHash,
			nullableInt64(document.CreatedAtMillis),
			nullableBool(document.IsArchived),
			*document.VectorRow,
			document.Dimensions,
			nullableInt64(document.EmbeddedAtMillis),
		); executeError != nil {
			return fmt.Errorf("save search document %s: %w", document.ID, executeError)
		}
		if _, ftsError := transaction.ExecContext(
			contextValue,
			`INSERT INTO search_documents_fts(search_index_id, document_id, conversation_title, document_text)
			 VALUES (?, ?, ?, ?)`,
			indexID,
			document.ID,
			document.ConversationTitle,
			document.Text,
		); ftsError != nil {
			return fmt.Errorf("save search FTS document %s: %w", document.ID, ftsError)
		}
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit search documents: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) DeleteSearchDocuments(contextValue context.Context, indexID int64, documentIDs []string) error {
	if len(documentIDs) == 0 {
		return nil
	}
	transaction, beginError := store.database.BeginTx(contextValue, nil)
	if beginError != nil {
		return fmt.Errorf("begin deleted search document transaction: %w", beginError)
	}
	rollbackRequired := true
	defer func() {
		if rollbackRequired {
			_ = transaction.Rollback()
		}
	}()
	for _, documentID := range documentIDs {
		if _, deleteError := transaction.ExecContext(
			contextValue,
			`DELETE FROM search_documents_fts WHERE search_index_id=? AND document_id=?`,
			indexID,
			documentID,
		); deleteError != nil {
			return fmt.Errorf("delete search FTS document %s: %w", documentID, deleteError)
		}
		if _, deleteError := transaction.ExecContext(
			contextValue,
			`DELETE FROM search_documents WHERE search_index_id=? AND document_id=?`,
			indexID,
			documentID,
		); deleteError != nil {
			return fmt.Errorf("delete search document %s: %w", documentID, deleteError)
		}
	}
	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("commit deleted search documents: %w", commitError)
	}
	rollbackRequired = false
	return nil
}

func (store *Store) CountSearchDocuments(contextValue context.Context, indexID int64) (int64, error) {
	var documentCount int64
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT COUNT(*) FROM search_documents WHERE search_index_id=?`,
		indexID,
	).Scan(&documentCount)
	if queryError != nil {
		return 0, fmt.Errorf("count search documents: %w", queryError)
	}
	return documentCount, nil
}

func (store *Store) ListSearchDocumentsForScan(
	contextValue context.Context,
	indexID int64,
	sinceMillis int64,
	untilMillis int64,
	includeArchived bool,
) ([]domain.SearchDocument, error) {
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT document_id, conversation_id, anchor_message_id, conversation_title,
		        role, content_type, source_text, document_text, text_hash,
		        created_at_ms, is_archived, vector_row, dimensions, embedded_at_ms
		 FROM search_documents
		 WHERE search_index_id=?
		   AND (? = 0 OR COALESCE(created_at_ms, 0) >= ?)
		   AND (? = 0 OR COALESCE(created_at_ms, 0) < ?)
		   AND (? = 1 OR COALESCE(is_archived, 0) = 0)
		 ORDER BY vector_row`,
		indexID,
		sinceMillis,
		sinceMillis,
		untilMillis,
		untilMillis,
		includeArchived,
	)
	if queryError != nil {
		return nil, fmt.Errorf("list search documents for scan: %w", queryError)
	}
	defer rows.Close()
	documents := make([]domain.SearchDocument, 0)
	for rows.Next() {
		document, scanError := scanSearchDocument(rows, indexID)
		if scanError != nil {
			return nil, scanError
		}
		documents = append(documents, document)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate search documents for scan: %w", rowsError)
	}
	return documents, nil
}

func (store *Store) SearchLexicalDocuments(
	contextValue context.Context,
	indexID int64,
	ftsQuery string,
	sinceMillis int64,
	untilMillis int64,
	includeArchived bool,
	limit int,
) ([]domain.LexicalSearchHit, error) {
	if limit <= 0 {
		limit = 1000000
	}
	rows, queryError := store.database.QueryContext(
		contextValue,
		`SELECT document.document_id, document.conversation_id, document.anchor_message_id,
		        document.conversation_title, document.role, document.content_type,
		        document.source_text, document.document_text, document.text_hash,
		        document.created_at_ms, document.is_archived, document.vector_row,
		        document.dimensions, document.embedded_at_ms,
		        bm25(search_documents_fts)
		 FROM search_documents_fts
		 JOIN search_documents document
		   ON document.search_index_id=search_documents_fts.search_index_id
		  AND document.document_id=search_documents_fts.document_id
		 WHERE search_documents_fts MATCH ?
		   AND document.search_index_id=?
		   AND (? = 0 OR COALESCE(document.created_at_ms, 0) >= ?)
		   AND (? = 0 OR COALESCE(document.created_at_ms, 0) < ?)
		   AND (? = 1 OR COALESCE(document.is_archived, 0) = 0)
		 ORDER BY bm25(search_documents_fts)
		 LIMIT ?`,
		ftsQuery,
		indexID,
		sinceMillis,
		sinceMillis,
		untilMillis,
		untilMillis,
		includeArchived,
		limit,
	)
	if queryError != nil {
		return nil, fmt.Errorf("search retrieval documents with FTS: %w", queryError)
	}
	defer rows.Close()
	hits := make([]domain.LexicalSearchHit, 0)
	for rows.Next() {
		var document domain.SearchDocument
		document.IndexID = indexID
		var createdAt sql.NullInt64
		var archived sql.NullInt64
		var vectorRow sql.NullInt64
		var embeddedAt sql.NullInt64
		var hit domain.LexicalSearchHit
		if scanError := rows.Scan(
			&document.ID,
			&document.ConversationID,
			&document.AnchorMessageID,
			&document.ConversationTitle,
			&document.Role,
			&document.ContentType,
			&document.SourceText,
			&document.Text,
			&document.TextHash,
			&createdAt,
			&archived,
			&vectorRow,
			&document.Dimensions,
			&embeddedAt,
			&hit.Score,
		); scanError != nil {
			return nil, fmt.Errorf("read lexical search hit: %w", scanError)
		}
		document.CreatedAtMillis = nullableInt64Pointer(createdAt)
		document.IsArchived = nullableBoolPointer(archived)
		document.VectorRow = nullableInt64Pointer(vectorRow)
		document.EmbeddedAtMillis = nullableInt64Pointer(embeddedAt)
		hit.Document = document
		hits = append(hits, hit)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate lexical search hits: %w", rowsError)
	}
	return hits, nil
}

func (store *Store) LoadQueryEmbedding(contextValue context.Context, indexID int64, queryHash string) ([]float32, bool, error) {
	var vectorBytes []byte
	var dimensions int
	queryError := store.database.QueryRowContext(
		contextValue,
		`SELECT vector_blob, dimensions FROM query_embedding_cache WHERE search_index_id=? AND query_hash=?`,
		indexID,
		queryHash,
	).Scan(&vectorBytes, &dimensions)
	if queryError != nil {
		if queryError == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load query embedding: %w", queryError)
	}
	if len(vectorBytes) != dimensions*4 {
		return nil, false, fmt.Errorf("cached query embedding has %d bytes for %d dimensions", len(vectorBytes), dimensions)
	}
	vector := make([]float32, dimensions)
	for dimensionIndex := range vector {
		vector[dimensionIndex] = math.Float32frombits(binary.LittleEndian.Uint32(vectorBytes[dimensionIndex*4:]))
	}
	return vector, true, nil
}

func (store *Store) SaveQueryEmbedding(contextValue context.Context, indexID int64, queryHash string, queryText string, vector []float32) error {
	vectorBytes := make([]byte, len(vector)*4)
	for vectorIndex, vectorValue := range vector {
		binary.LittleEndian.PutUint32(vectorBytes[vectorIndex*4:], math.Float32bits(vectorValue))
	}
	_, executeError := store.database.ExecContext(
		contextValue,
		`INSERT INTO query_embedding_cache(
			search_index_id, query_hash, query_text, vector_blob, dimensions, created_at_ms
		 ) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(search_index_id, query_hash) DO UPDATE SET
			query_text=excluded.query_text,
			vector_blob=excluded.vector_blob,
			dimensions=excluded.dimensions,
			created_at_ms=excluded.created_at_ms`,
		indexID,
		queryHash,
		queryText,
		vectorBytes,
		len(vector),
		time.Now().UTC().UnixMilli(),
	)
	if executeError != nil {
		return fmt.Errorf("save query embedding: %w", executeError)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanSearchDocument(scanner rowScanner, indexID int64) (domain.SearchDocument, error) {
	var document domain.SearchDocument
	document.IndexID = indexID
	var createdAt sql.NullInt64
	var archived sql.NullInt64
	var vectorRow sql.NullInt64
	var embeddedAt sql.NullInt64
	if scanError := scanner.Scan(
		&document.ID,
		&document.ConversationID,
		&document.AnchorMessageID,
		&document.ConversationTitle,
		&document.Role,
		&document.ContentType,
		&document.SourceText,
		&document.Text,
		&document.TextHash,
		&createdAt,
		&archived,
		&vectorRow,
		&document.Dimensions,
		&embeddedAt,
	); scanError != nil {
		return document, fmt.Errorf("read search document: %w", scanError)
	}
	document.CreatedAtMillis = nullableInt64Pointer(createdAt)
	document.IsArchived = nullableBoolPointer(archived)
	document.VectorRow = nullableInt64Pointer(vectorRow)
	document.EmbeddedAtMillis = nullableInt64Pointer(embeddedAt)
	return document, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return 1
	}
	return 0
}
