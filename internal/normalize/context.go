package normalize

import (
	"regexp"
	"strings"
)

var contextualReferencePattern = regexp.MustCompile(`(?i)\b(this|that|it|these|those|this word|that word|this phrase|that phrase|what does this mean|what does that mean|what is this|what is that)\b`)

func NeedsContext(messageText string) bool {
	trimmedText := strings.TrimSpace(messageText)
	if trimmedText == "" {
		return false
	}
	wordCount := len(strings.Fields(trimmedText))
	return wordCount < 20 && contextualReferencePattern.MatchString(trimmedText)
}

func BuildSearchText(conversationTitle string, userText string, parentText string, followingText string) string {
	trimmedUserText := strings.TrimSpace(userText)
	if !NeedsContext(trimmedUserText) {
		return TruncateUTF8("USER:\n"+trimmedUserText, 6000)
	}

	sections := make([]string, 0, 4)
	if strings.TrimSpace(conversationTitle) != "" {
		sections = append(sections, "CONVERSATION TITLE:\n"+TruncateUTF8(strings.TrimSpace(conversationTitle), 300))
	}
	if strings.TrimSpace(parentText) != "" {
		sections = append(sections, "PREVIOUS MESSAGE:\n"+TruncateUTF8(strings.TrimSpace(parentText), 1800))
	}
	sections = append(sections, "USER:\n"+TruncateUTF8(trimmedUserText, 3000))
	if strings.TrimSpace(followingText) != "" {
		sections = append(sections, "FOLLOWING ASSISTANT:\n"+TruncateUTF8(strings.TrimSpace(followingText), 1800))
	}
	return TruncateUTF8(strings.Join(sections, "\n\n"), 6500)
}
