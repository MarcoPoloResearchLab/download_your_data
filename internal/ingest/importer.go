package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/exportformat"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

type Progress struct {
	SourceFile        string
	ConversationsSeen int64
	MessagesSeen      int64
	WarningsCount     int64
}

type Result struct {
	ImportID          int64
	SourceHash        string
	Skipped           bool
	ConversationsSeen int64
	MessagesSeen      int64
	WarningsCount     int64
}

type Importer struct {
	Store    *store.Store
	Progress func(Progress)
}

func (importer *Importer) Import(contextValue context.Context, sourcePath string, force bool) (Result, error) {
	result := Result{}
	collection, discoverError := exportformat.DiscoverSources(sourcePath)
	if discoverError != nil {
		return result, discoverError
	}
	defer collection.Close()

	sourceHash, hashError := hashSources(collection)
	if hashError != nil {
		return result, hashError
	}
	result.SourceHash = sourceHash

	completedExists, existingError := importer.Store.CompletedImportExists(contextValue, sourceHash, domain.ParserVersion)
	if existingError != nil {
		return result, existingError
	}
	if completedExists && !force {
		result.Skipped = true
		return result, nil
	}

	importID, beginError := importer.Store.BeginImport(contextValue, sourcePath, sourceHash, domain.ParserVersion)
	if beginError != nil {
		return result, beginError
	}
	result.ImportID = importID
	importCompleted := false
	defer func() {
		if !importCompleted {
			_ = importer.Store.FailImport(context.Background(), importID)
		}
	}()

	for _, source := range collection.Sources {
		sourceReader, openError := source.Open()
		if openError != nil {
			return result, fmt.Errorf("open %s: %w", source.Name, openError)
		}
		streamError := exportformat.StreamConversations(sourceReader, func(rawConversation exportformat.RawConversation) error {
			conversation, conversionWarnings := convertConversation(rawConversation, source.Name)
			messageCount, storageWarnings, storageError := importer.Store.ImportConversation(contextValue, importID, conversation)
			if storageError != nil {
				return storageError
			}
			result.ConversationsSeen++
			result.MessagesSeen += messageCount
			result.WarningsCount += conversionWarnings + storageWarnings

			if result.ConversationsSeen%100 == 0 {
				if progressError := importer.Store.UpdateImportProgress(
					contextValue,
					importID,
					result.ConversationsSeen,
					result.MessagesSeen,
					result.WarningsCount,
				); progressError != nil {
					return progressError
				}
			}
			if importer.Progress != nil {
				importer.Progress(Progress{
					SourceFile:        source.Name,
					ConversationsSeen: result.ConversationsSeen,
					MessagesSeen:      result.MessagesSeen,
					WarningsCount:     result.WarningsCount,
				})
			}
			return nil
		})
		closeError := sourceReader.Close()
		if streamError != nil {
			return result, fmt.Errorf("import %s: %w", source.Name, streamError)
		}
		if closeError != nil {
			return result, fmt.Errorf("close %s: %w", source.Name, closeError)
		}
	}

	if completionError := importer.Store.CompleteImport(
		contextValue,
		importID,
		result.ConversationsSeen,
		result.MessagesSeen,
		result.WarningsCount,
	); completionError != nil {
		return result, completionError
	}
	importCompleted = true
	return result, nil
}

func hashSources(collection *exportformat.SourceCollection) (string, error) {
	hashWriter := sha256.New()
	for _, source := range collection.Sources {
		hashWriter.Write([]byte(source.Name))
		hashWriter.Write([]byte{0})
		sourceReader, openError := source.Open()
		if openError != nil {
			return "", fmt.Errorf("open %s for hashing: %w", source.Name, openError)
		}
		_, copyError := io.Copy(hashWriter, sourceReader)
		closeError := sourceReader.Close()
		if copyError != nil {
			return "", fmt.Errorf("hash %s: %w", source.Name, copyError)
		}
		if closeError != nil {
			return "", fmt.Errorf("close %s after hashing: %w", source.Name, closeError)
		}
	}
	return hex.EncodeToString(hashWriter.Sum(nil)), nil
}

func convertConversation(rawConversation exportformat.RawConversation, sourceFile string) (domain.Conversation, int64) {
	conversationID := strings.TrimSpace(rawConversation.ID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(rawConversation.ConversationID)
	}
	if conversationID == "" {
		conversationID = "generated-conversation-" + normalize.Hash(
			rawConversation.Title,
			rawConversation.CreateTime.Time.String(),
			sourceFile,
		)[:24]
	}

	archiveValue := rawConversation.IsArchived
	if archiveValue == nil {
		archiveValue = rawConversation.Archived
	}
	conversationMetadata := marshalMetadata(rawConversation.Metadata)
	conversation := domain.Conversation{
		ID:            conversationID,
		Title:         strings.TrimSpace(rawConversation.Title),
		CreatedAt:     rawConversation.CreateTime.Pointer(),
		UpdatedAt:     rawConversation.UpdateTime.Pointer(),
		IsArchived:    archiveValue,
		CurrentNodeID: rawConversation.CurrentNode,
		SourceFile:    sourceFile,
		RawMetadata:   conversationMetadata,
		Messages:      make([]domain.Message, 0, len(rawConversation.Mapping)),
		Edges:         make([]domain.MessageEdge, 0),
	}

	nodeKeys := make([]string, 0, len(rawConversation.Mapping))
	for nodeKey := range rawConversation.Mapping {
		nodeKeys = append(nodeKeys, nodeKey)
	}
	sort.Slice(nodeKeys, func(leftIndex int, rightIndex int) bool {
		leftNode := rawConversation.Mapping[nodeKeys[leftIndex]]
		rightNode := rawConversation.Mapping[nodeKeys[rightIndex]]
		leftTime := int64(0)
		rightTime := int64(0)
		if leftNode.Message != nil && leftNode.Message.CreateTime.Valid {
			leftTime = leftNode.Message.CreateTime.Time.UnixMilli()
		}
		if rightNode.Message != nil && rightNode.Message.CreateTime.Valid {
			rightTime = rightNode.Message.CreateTime.Time.UnixMilli()
		}
		if leftTime == rightTime {
			return nodeKeys[leftIndex] < nodeKeys[rightIndex]
		}
		return leftTime < rightTime
	})

	var warningsCount int64
	for _, nodeKey := range nodeKeys {
		rawNode := rawConversation.Mapping[nodeKey]
		// The mapping key is the occurrence identity inside a conversation.
		// Raw message IDs are not globally unique in large OpenAI exports and
		// can be reused across conversations or branches.
		nodeID := strings.TrimSpace(nodeKey)
		if nodeID == "" {
			nodeID = strings.TrimSpace(rawNode.ID)
		}
		if nodeID == "" {
			nodeID = "generated-node-" + normalize.Hash(conversationID, sourceFile, fmt.Sprintf("%d", len(conversation.Messages)))[:24]
		}
		for _, childNodeID := range rawNode.Children {
			if strings.TrimSpace(childNodeID) == "" {
				continue
			}
			conversation.Edges = append(conversation.Edges, domain.MessageEdge{
				ConversationID: conversationID,
				ParentNodeID:   nodeID,
				ChildNodeID:    childNodeID,
			})
		}
		if rawNode.Message == nil {
			continue
		}

		extractedContent := exportformat.ExtractContent(rawNode.Message.Content)
		normalizedText := normalize.Text(extractedContent.Text)
		sourceMessageID := strings.TrimSpace(rawNode.Message.ID)
		messageID := occurrenceMessageID(conversationID, nodeID)
		parentNodeID := ""
		if rawNode.Parent != nil {
			parentNodeID = strings.TrimSpace(*rawNode.Parent)
		}
		warningText := strings.Join(extractedContent.Warnings, "; ")
		if warningText != "" {
			warningsCount++
		}

		createdAt := rawNode.Message.CreateTime.Pointer()
		if createdAt == nil {
			createdAt = rawConversation.CreateTime.Pointer()
		}
		messageMetadata := marshalMetadata(rawNode.Message.Metadata)
		conversation.Messages = append(conversation.Messages, domain.Message{
			ID:                messageID,
			SourceMessageID:   sourceMessageID,
			ConversationID:    conversationID,
			ParentNodeID:      parentNodeID,
			Role:              strings.TrimSpace(rawNode.Message.Author.Role),
			CreatedAt:         createdAt,
			ContentType:       extractedContent.ContentType,
			OriginalText:      extractedContent.Text,
			NormalizedText:    normalizedText,
			ContentHash:       normalize.Hash(normalizedText),
			SourceFile:        sourceFile,
			SourceNodeID:      nodeID,
			ExtractionWarning: warningText,
			RawMetadata:       messageMetadata,
		})
	}
	return conversation, warningsCount
}

func occurrenceMessageID(conversationID string, sourceNodeID string) string {
	return "occ_" + normalize.Hash("message-occurrence-v1", conversationID, sourceNodeID)
}

func marshalMetadata(metadata map[string]json.RawMessage) string {
	if len(metadata) == 0 {
		return ""
	}
	encodedMetadata, marshalError := json.Marshal(metadata)
	if marshalError != nil {
		return ""
	}
	return string(encodedMetadata)
}
