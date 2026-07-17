package retrieval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/domain"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const (
	DefaultIndexName      = "conversation-search"
	DefaultDocumentPrefix = "search_document: "
	DefaultQueryPrefix    = "search_query: "
	DefaultBuilderVersion = "1"
	DefaultCorpusPolicy   = "visible-user-assistant-text-v1"
)

func BuildDocuments(contextValue context.Context, openedStore *store.Store) ([]domain.SearchDocument, error) {
	if openedStore == nil {
		return nil, fmt.Errorf("document builder requires a store")
	}
	nodes, nodeError := openedStore.ListRetrievalSourceNodes(contextValue)
	if nodeError != nil {
		return nil, nodeError
	}
	edges, edgeError := openedStore.ListMessageEdges(contextValue)
	if edgeError != nil {
		return nil, edgeError
	}

	nodeByKey := make(map[string]domain.RetrievalSourceNode, len(nodes))
	parentByChild := make(map[string]string, len(edges))
	for _, node := range nodes {
		nodeByKey[nodeKey(node.ConversationID, node.SourceNodeID)] = node
		if strings.TrimSpace(node.ParentNodeID) != "" {
			parentByChild[nodeKey(node.ConversationID, node.SourceNodeID)] = node.ParentNodeID
		}
	}
	for _, edge := range edges {
		parentByChild[nodeKey(edge.ConversationID, edge.ChildNodeID)] = edge.ParentNodeID
	}

	documents := make([]domain.SearchDocument, 0, len(nodes))
	for _, node := range nodes {
		if !isSearchableNode(node) {
			continue
		}
		previousNodes := visibleAncestors(node, nodeByKey, parentByChild, 2)
		documentText := buildDocumentText(node, previousNodes)
		documents = append(documents, domain.SearchDocument{
			ID:                normalize.Hash("conversation-search-document", DefaultBuilderVersion, node.MessageID),
			ConversationID:    node.ConversationID,
			AnchorMessageID:   node.MessageID,
			ConversationTitle: node.ConversationTitle,
			Role:              node.Role,
			ContentType:       node.ContentType,
			SourceText:        node.OriginalText,
			Text:              documentText,
			TextHash: normalize.Hash(
				DefaultBuilderVersion,
				DefaultCorpusPolicy,
				documentText,
				"created="+optionalInt64Identity(node.CreatedAtMillis),
				"archived="+optionalBoolIdentity(node.IsArchived),
			),
			CreatedAtMillis: node.CreatedAtMillis,
			IsArchived:      node.IsArchived,
		})
	}
	sort.Slice(documents, func(leftIndex int, rightIndex int) bool {
		return documents[leftIndex].ID < documents[rightIndex].ID
	})
	return documents, nil
}

func isSearchableNode(node domain.RetrievalSourceNode) bool {
	if node.Role != "user" && node.Role != "assistant" {
		return false
	}
	if node.ContentType != "text" && node.ContentType != "multimodal_text" {
		return false
	}
	return strings.TrimSpace(node.NormalizedText) != ""
}

func visibleAncestors(
	node domain.RetrievalSourceNode,
	nodeByKey map[string]domain.RetrievalSourceNode,
	parentByChild map[string]string,
	maximumVisible int,
) []domain.RetrievalSourceNode {
	ancestors := make([]domain.RetrievalSourceNode, 0, maximumVisible)
	visited := make(map[string]struct{})
	currentNodeID := parentByChild[nodeKey(node.ConversationID, node.SourceNodeID)]
	for currentNodeID != "" && len(ancestors) < maximumVisible {
		key := nodeKey(node.ConversationID, currentNodeID)
		if _, exists := visited[key]; exists {
			break
		}
		visited[key] = struct{}{}
		ancestor, exists := nodeByKey[key]
		if exists && isSearchableNode(ancestor) {
			ancestors = append(ancestors, ancestor)
		}
		nextParent := parentByChild[key]
		if nextParent == "" && exists {
			nextParent = ancestor.ParentNodeID
		}
		currentNodeID = nextParent
	}
	for leftIndex, rightIndex := 0, len(ancestors)-1; leftIndex < rightIndex; leftIndex, rightIndex = leftIndex+1, rightIndex-1 {
		ancestors[leftIndex], ancestors[rightIndex] = ancestors[rightIndex], ancestors[leftIndex]
	}
	return ancestors
}

func buildDocumentText(node domain.RetrievalSourceNode, previous []domain.RetrievalSourceNode) string {
	sections := make([]string, 0, len(previous)+2)
	if title := strings.TrimSpace(node.ConversationTitle); title != "" {
		sections = append(sections, "CONVERSATION TITLE:\n"+normalize.TruncateUTF8(title, 300))
	}
	for _, ancestor := range previous {
		label := "PREVIOUS " + strings.ToUpper(ancestor.Role)
		sections = append(sections, label+":\n"+normalize.TruncateUTF8(strings.TrimSpace(ancestor.OriginalText), 1600))
	}
	currentLabel := "CURRENT " + strings.ToUpper(node.Role)
	sections = append(sections, currentLabel+":\n"+normalize.TruncateUTF8(strings.TrimSpace(node.OriginalText), 4000))
	return normalize.TruncateUTF8(strings.Join(sections, "\n\n"), 7000)
}

func nodeKey(conversationID string, nodeID string) string {
	return conversationID + "\x00" + nodeID
}

func optionalInt64Identity(value *int64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value)
}

func optionalBoolIdentity(value *bool) string {
	if value == nil {
		return "unknown"
	}
	if *value {
		return "true"
	}
	return "false"
}
