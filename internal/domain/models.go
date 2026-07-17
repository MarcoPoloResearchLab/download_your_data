package domain

import "time"

const (
	ParserVersion        = "2"
	PreprocessingVersion = "2"
	ContextVersion       = "2"
)

type Conversation struct {
	ID            string
	Title         string
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
	IsArchived    *bool
	CurrentNodeID string
	SourceFile    string
	RawMetadata   string
	Messages      []Message
	Edges         []MessageEdge
}

type Message struct {
	ID                string
	SourceMessageID   string
	ConversationID    string
	ParentNodeID      string
	Role              string
	CreatedAt         *time.Time
	ContentType       string
	OriginalText      string
	NormalizedText    string
	ContentHash       string
	SourceFile        string
	SourceNodeID      string
	ExtractionWarning string
	RawMetadata       string
}

type MessageEdge struct {
	ConversationID string
	ParentNodeID   string
	ChildNodeID    string
}

type EmbeddingCandidate struct {
	MessageID          string
	ConversationID     string
	ConversationTitle  string
	OriginalText       string
	NormalizedText     string
	CreatedAtMillis    *int64
	IsArchived         *bool
	ParentText         string
	FollowingText      string
	ExistingSearchHash string
}

type EmbeddingConfig struct {
	ID                   int64
	Provider             string
	Model                string
	Dimensions           int
	BaseURL              string
	InputPrefix          string
	VectorPath           string
	PreprocessingVersion string
	ContextVersion       string
	Status               string
	CreatedAtMillis      int64
	CompletedAtMillis    *int64
}

type EmbeddingConfigSummary struct {
	Config         EmbeddingConfig
	EmbeddingCount int64
	EligibleCount  int64
}

type EmbeddingRecord struct {
	MessageID       string
	VectorRow       int64
	Dimensions      int
	SearchTextHash  string
	CreatedAtMillis int64
}

type RetrievalSourceNode struct {
	MessageID         string
	ConversationID    string
	ConversationTitle string
	SourceNodeID      string
	ParentNodeID      string
	Role              string
	ContentType       string
	OriginalText      string
	NormalizedText    string
	CreatedAtMillis   *int64
	IsArchived        *bool
}

type SearchIndexConfig struct {
	ID                int64
	Name              string
	Provider          string
	Model             string
	Dimensions        int
	BaseURL           string
	DocumentPrefix    string
	QueryPrefix       string
	VectorPath        string
	BuilderVersion    string
	CorpusPolicy      string
	Status            string
	CreatedAtMillis   int64
	CompletedAtMillis *int64
}

type SearchIndexSummary struct {
	Config                SearchIndexConfig
	DocumentCount         int64
	EligibleCount         int64
	CoveredConversations  int64
	EligibleConversations int64
}

type SearchDocument struct {
	ID                string
	IndexID           int64
	ConversationID    string
	AnchorMessageID   string
	ConversationTitle string
	Role              string
	ContentType       string
	SourceText        string
	Text              string
	TextHash          string
	CreatedAtMillis   *int64
	IsArchived        *bool
	VectorRow         *int64
	Dimensions        int
	EmbeddedAtMillis  *int64
}

type SearchDocumentState struct {
	TextHash  string
	VectorRow *int64
}

type LexicalSearchHit struct {
	Document SearchDocument
	Score    float64
}

type ConversationSearchExcerpt struct {
	MessageID        string   `json:"message_id"`
	Role             string   `json:"role"`
	Text             string   `json:"text"`
	SemanticScore    float64  `json:"semantic_score"`
	LexicalScore     float64  `json:"lexical_score"`
	DetectionMethods []string `json:"detection_methods"`
}

type ConversationSearchResult struct {
	ConversationID    string                      `json:"conversation_id"`
	ConversationTitle string                      `json:"conversation_title"`
	Archived          *bool                       `json:"archived"`
	Score             float64                     `json:"score"`
	SemanticScore     float64                     `json:"semantic_score"`
	LexicalScore      float64                     `json:"lexical_score"`
	Excerpts          []ConversationSearchExcerpt `json:"excerpts"`
}

type SearchMessage struct {
	MessageID         string
	SourceMessageID   string
	ConversationID    string
	ConversationTitle string
	OriginalText      string
	NormalizedText    string
	CreatedAtMillis   *int64
	IsArchived        *bool
	ParentText        string
	FollowingText     string
	VectorRow         *int64
}

type DefinitionResult struct {
	DateISO             string   `json:"date"`
	Term                string   `json:"term"`
	Category            string   `json:"category"`
	ExactUserMessage    string   `json:"exact_user_message"`
	ConversationTitle   string   `json:"conversation_title"`
	Archived            *bool    `json:"archived"`
	Confidence          float64  `json:"classification_confidence"`
	DetectionMethods    []string `json:"detection_methods"`
	ConversationID      string   `json:"conversation_id"`
	MessageID           string   `json:"message_id"`
	SourceMessageID     string   `json:"source_message_id,omitempty"`
	SemanticPositive    float64  `json:"semantic_positive"`
	SemanticNegative    float64  `json:"semantic_negative"`
	SemanticMargin      float64  `json:"semantic_margin"`
	NeedsReview         bool     `json:"needs_review"`
	VerifierExplanation string   `json:"verifier_explanation,omitempty"`
}

type StoreStats struct {
	Imports                 int64
	LatestImportMessages    int64
	Conversations           int64
	ArchivedConversations   int64
	Messages                int64
	UserMessages            int64
	AssistantMessages       int64
	SourceMessageIDs        int64
	UniqueSourceMessageIDs  int64
	RepeatedSourceMessages  int64
	MessagesWithoutSourceID int64
	EmbeddingConfigurations int64
	Embeddings              int64
}
