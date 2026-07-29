package intent

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type LexicalMatch struct {
	Matched  bool
	Score    float64
	Term     string
	Category string
	Methods  []string
}

type CompiledRule struct {
	Rule       LexicalRule
	Expression *regexp.Regexp
}

func CompileRules(rules []LexicalRule) ([]CompiledRule, error) {
	compiledRules := make([]CompiledRule, 0, len(rules))
	for _, rule := range rules {
		expression, compileError := regexp.Compile(rule.Pattern)
		if compileError != nil {
			return nil, fmt.Errorf("compile lexical rule %s: %w", rule.Name, compileError)
		}
		compiledRules = append(compiledRules, CompiledRule{Rule: rule, Expression: expression})
	}
	return compiledRules, nil
}

func EvaluateLexical(messageText string, compiledRules []CompiledRule) LexicalMatch {
	bestMatch := LexicalMatch{}
	for _, compiledRule := range compiledRules {
		submatches := compiledRule.Expression.FindStringSubmatch(messageText)
		if len(submatches) == 0 {
			continue
		}
		term := ""
		if compiledRule.Rule.TermGroup > 0 && compiledRule.Rule.TermGroup < len(submatches) {
			term = cleanTerm(submatches[compiledRule.Rule.TermGroup])
		}
		if compiledRule.Rule.Score > bestMatch.Score {
			bestMatch = LexicalMatch{
				Matched:  true,
				Score:    compiledRule.Rule.Score,
				Term:     term,
				Category: compiledRule.Rule.Category,
				Methods:  []string{"lexical:" + compiledRule.Rule.Name},
			}
		} else if compiledRule.Rule.Score == bestMatch.Score {
			bestMatch.Methods = append(bestMatch.Methods, "lexical:"+compiledRule.Rule.Name)
			if bestMatch.Term == "" {
				bestMatch.Term = term
			}
		}
	}
	if bestMatch.Category == "" {
		bestMatch.Category = inferCategory(bestMatch.Term, messageText)
	}
	return bestMatch
}

func cleanTerm(value string) string {
	trimmedValue := strings.TrimSpace(value)
	trimmedValue = strings.Trim(trimmedValue, " \t\r\n\"'“”‘’`.,!?;:()[]{}")
	trimmedValue = strings.Join(strings.Fields(trimmedValue), " ")
	if len([]rune(trimmedValue)) > 120 {
		trimmedRunes := []rune(trimmedValue)
		trimmedValue = string(trimmedRunes[:120])
	}
	return trimmedValue
}

func inferCategory(term string, messageText string) string {
	lowerMessage := strings.ToLower(messageText)
	if strings.Contains(lowerMessage, "grammar") || strings.Contains(lowerMessage, "tense") || strings.Contains(lowerMessage, "grammatical") {
		return "grammar"
	}
	if strings.Contains(lowerMessage, "used here") || strings.Contains(lowerMessage, "usage") || strings.Contains(lowerMessage, "another word") {
		return "usage"
	}
	if strings.Contains(lowerMessage, "idiom") || strings.Contains(lowerMessage, "expression") {
		return "idiom"
	}
	termWords := strings.FieldsFunc(term, func(currentRune rune) bool {
		return unicode.IsSpace(currentRune)
	})
	if len(termWords) > 1 {
		return "phrase"
	}
	if len(termWords) == 1 {
		return "word"
	}
	return "ambiguous"
}

var quotedTermPattern = regexp.MustCompile(`["“']([[:alnum:]][[:alnum:]_'-]*(?:\s+[[:alnum:]][[:alnum:]_'-]*){0,4})["”']`)
var namedTermPattern = regexp.MustCompile(`(?i)\b(?:word|term|phrase|expression)\s+["“']?([[:alnum:]][[:alnum:]_'-]*(?:\s+[[:alnum:]][[:alnum:]_'-]*){0,4})`)
var leadingDefinitionPattern = regexp.MustCompile(`(?i)^\s*(?:(?:a|an|the)\s+)?([[:alpha:]][[:alpha:]'-]*(?:\s+[[:alpha:]][[:alpha:]'-]*){0,2})\s+(?:can\s+mean|means|refers\s+to|is\b)`)
var titleDefinitionPattern = regexp.MustCompile(`(?i)^\s*(?:define|meaning of|definition of)\s+(.+?)\s*$`)

func resolveContextualTerm(term string, previousMessage string, followingMessage string, conversationTitle string) (string, bool) {
	normalizedTerm := strings.ToLower(strings.TrimSpace(term))
	contextualTerms := map[string]struct{}{
		"this": {}, "that": {}, "it": {}, "this word": {}, "that word": {},
		"this phrase": {}, "that phrase": {}, "this term": {}, "that term": {},
	}
	if _, isContextual := contextualTerms[normalizedTerm]; !isContextual {
		return term, false
	}

	contextValues := []string{previousMessage, followingMessage}
	for _, contextValue := range contextValues {
		if matches := quotedTermPattern.FindStringSubmatch(contextValue); len(matches) > 1 {
			return cleanTerm(matches[1]), true
		}
		if matches := namedTermPattern.FindStringSubmatch(contextValue); len(matches) > 1 {
			return cleanTerm(matches[1]), true
		}
		if matches := leadingDefinitionPattern.FindStringSubmatch(contextValue); len(matches) > 1 {
			return cleanTerm(matches[1]), true
		}
	}
	if matches := titleDefinitionPattern.FindStringSubmatch(conversationTitle); len(matches) > 1 {
		return cleanTerm(matches[1]), true
	}
	return term, false
}
