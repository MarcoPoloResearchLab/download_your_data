package intent

type LexicalRule struct {
	Name      string  `json:"name"`
	Pattern   string  `json:"pattern"`
	Score     float64 `json:"score"`
	TermGroup int     `json:"term_group"`
	Category  string  `json:"category"`
}

type DefinitionConfig struct {
	Name               string        `json:"name"`
	Description        string        `json:"description"`
	PositiveExamples   []string      `json:"positive_examples"`
	NegativeExamples   []string      `json:"negative_examples"`
	LexicalRules       []LexicalRule `json:"lexical_rules"`
	SemanticThreshold  float64       `json:"semantic_threshold"`
	SemanticMargin     float64       `json:"semantic_margin"`
	ReviewThreshold    float64       `json:"review_threshold"`
	LexicalReviewScore float64       `json:"lexical_review_score"`
}

func DefaultDefinitionConfig() DefinitionConfig {
	return DefinitionConfig{
		Name:        "definition_request",
		Description: "A request for the meaning, definition, usage, interpretation, or grammatical meaning of a word, phrase, idiom, or technical term.",
		PositiveExamples: []string{
			"define incredulous",
			"what does this word mean",
			"what does stoppage time mean",
			"explain the meaning of this phrase",
			"I don't understand the word preening",
			"is sauntered another word for walked",
			"what is meant by berth",
			"how is the word wily used here",
			"what is the difference between these two meanings",
			"what does that expression mean in this sentence",
		},
		NegativeExamples: []string{
			"what is the weather tomorrow",
			"explain how Docker works",
			"why did the stock decline",
			"what happened during the war",
			"how can I implement this algorithm",
			"what is the best school nearby",
			"summarize this article",
			"what is on my calendar today",
		},
		LexicalRules: []LexicalRule{
			{Name: "define", Pattern: `(?i)^\s*(?:please\s+)?define\s+["'“”]?(.+?)["'“”]?\s*[?.!]*\s*$`, Score: 1.0, TermGroup: 1},
			{Name: "what_does_mean", Pattern: `(?i)\bwhat\s+does\s+["'“”]?(.+?)["'“”]?\s+mean\b`, Score: 1.0, TermGroup: 1},
			{Name: "meaning_of", Pattern: `(?i)\b(?:the\s+)?meaning\s+of\s+["'“”]?(.+?)["'“”]?(?:\?|$)`, Score: 0.98, TermGroup: 1},
			{Name: "meant_by", Pattern: `(?i)\bwhat\s+is\s+meant\s+by\s+["'“”]?(.+?)["'“”]?(?:\?|$)`, Score: 0.98, TermGroup: 1},
			{Name: "explain_meaning", Pattern: `(?i)\bexplain\s+(?:the\s+)?meaning\s+of\s+["'“”]?(.+?)["'“”]?(?:\?|$)`, Score: 0.98, TermGroup: 1},
			{Name: "dont_understand_word", Pattern: `(?i)\bI\s+(?:do\s+not|don't)\s+understand\s+(?:the\s+)?(?:word|phrase|term|expression)?\s*["'“”]?(.+?)["'“”]?(?:\.|\?|$)`, Score: 0.9, TermGroup: 1},
			{Name: "used_here", Pattern: `(?i)\bhow\s+is\s+(?:the\s+)?(?:word|phrase|term)?\s*["'“”]?(.+?)["'“”]?\s+used\s+(?:here|in\s+this)\b`, Score: 0.9, TermGroup: 1, Category: "usage"},
			{Name: "another_word", Pattern: `(?i)\bis\s+["'“”]?(.+?)["'“”]?\s+(?:just\s+)?another\s+(?:word|way\s+to\s+say)\b`, Score: 0.9, TermGroup: 1, Category: "usage"},
			{Name: "what_is_term", Pattern: `(?i)^\s*(?:what\s+is|what's)\s+(?:the\s+)?(?:word|phrase|term|expression)\s+["'“”]?(.+?)["'“”]?\s*[?.!]*\s*$`, Score: 0.95, TermGroup: 1},
			{Name: "short_what_is", Pattern: `(?i)^\s*(?:what\s+is|what's)\s+["'“”]?([^?]{1,80}?)["'“”]?\s*\?\s*$`, Score: 0.62, TermGroup: 1},
		},
		SemanticThreshold:  0.43,
		SemanticMargin:     0.01,
		ReviewThreshold:    0.35,
		LexicalReviewScore: 0.55,
	}
}
