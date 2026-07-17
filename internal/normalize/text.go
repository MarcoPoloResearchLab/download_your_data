package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var excessiveBlankLinesPattern = regexp.MustCompile(`\n{3,}`)

func Text(value string) string {
	normalizedLineEndings := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	cleanedRunes := make([]rune, 0, len(normalizedLineEndings))
	for _, currentRune := range normalizedLineEndings {
		if currentRune == '\n' || currentRune == '\t' || !unicode.IsControl(currentRune) {
			cleanedRunes = append(cleanedRunes, currentRune)
		}
	}
	trimmedLines := trimLineEnds(string(cleanedRunes))
	collapsedLines := excessiveBlankLinesPattern.ReplaceAllString(trimmedLines, "\n\n")
	return strings.TrimSpace(collapsedLines)
}

func trimLineEnds(value string) string {
	lines := strings.Split(value, "\n")
	for lineIndex, line := range lines {
		lines[lineIndex] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}

func Hash(parts ...string) string {
	hashWriter := sha256.New()
	for _, part := range parts {
		hashWriter.Write([]byte(part))
		hashWriter.Write([]byte{0})
	}
	return hex.EncodeToString(hashWriter.Sum(nil))
}

func TruncateUTF8(value string, maximumRunes int) string {
	if maximumRunes <= 0 || utf8.RuneCountInString(value) <= maximumRunes {
		return value
	}
	valueRunes := []rune(value)
	return string(valueRunes[:maximumRunes]) + "\n[truncated]"
}
