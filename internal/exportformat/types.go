package exportformat

import "encoding/json"

type RawConversation struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversation_id"`
	Title          string             `json:"title"`
	CreateTime     FlexibleTimestamp  `json:"create_time"`
	UpdateTime     FlexibleTimestamp  `json:"update_time"`
	CurrentNode    string             `json:"current_node"`
	Mapping        map[string]RawNode `json:"mapping"`
	IsArchived     *bool              `json:"is_archived"`
	Archived       *bool              `json:"archived"`
}

type RawNode struct {
	ID       string      `json:"id"`
	Parent   *string     `json:"parent"`
	Children []string    `json:"children"`
	Message  *RawMessage `json:"message"`
}

type RawMessage struct {
	ID         string            `json:"id"`
	Author     RawAuthor         `json:"author"`
	CreateTime FlexibleTimestamp `json:"create_time"`
	UpdateTime FlexibleTimestamp `json:"update_time"`
	Content    json.RawMessage   `json:"content"`
}

type RawAuthor struct {
	Role string `json:"role"`
	Name string `json:"name"`
}
