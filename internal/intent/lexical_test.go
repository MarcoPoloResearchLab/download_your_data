package intent

import "testing"

func TestEvaluateLexicalDirectDefinition(testContext *testing.T) {
	config := DefaultDefinitionConfig()
	compiledRules, compileError := CompileRules(config.LexicalRules)
	if compileError != nil {
		testContext.Fatalf("compile rules: %v", compileError)
	}
	match := EvaluateLexical("what does stoppage time mean", compiledRules)
	if !match.Matched || match.Score < 0.99 {
		testContext.Fatalf("expected a high-confidence match, received %+v", match)
	}
	if match.Term != "stoppage time" {
		testContext.Fatalf("unexpected term: %q", match.Term)
	}
	if match.Category != "phrase" {
		testContext.Fatalf("unexpected category: %q", match.Category)
	}
}

func TestEvaluateLexicalBroadQuestionNeedsReview(testContext *testing.T) {
	config := DefaultDefinitionConfig()
	compiledRules, compileError := CompileRules(config.LexicalRules)
	if compileError != nil {
		testContext.Fatalf("compile rules: %v", compileError)
	}
	match := EvaluateLexical("What is the weather tomorrow?", compiledRules)
	if !match.Matched {
		testContext.Fatal("short what-is rule should retrieve the question for review")
	}
	if match.Score >= 0.82 {
		testContext.Fatalf("broad question should not be automatically accepted: %+v", match)
	}
}
