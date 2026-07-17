package intent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/normalize"
)

const VerificationPromptVersion = "2"

type VerificationInput struct {
	MessageID         string `json:"id"`
	ConversationTitle string `json:"conversation_title"`
	PreviousMessage   string `json:"previous_message"`
	UserMessage       string `json:"user_message"`
	FollowingMessage  string `json:"following_assistant_message"`
}

type VerificationResult struct {
	MessageID           string   `json:"id"`
	IsDefinitionRequest bool     `json:"is_definition_request"`
	Terms               []string `json:"terms"`
	Category            string   `json:"category"`
	Confidence          float64  `json:"confidence"`
	Explanation         string   `json:"explanation"`
}

type Verifier interface {
	Verify(context.Context, []VerificationInput) (map[string]VerificationResult, error)
}

type IdentifiedVerifier interface {
	Verifier
	Identity() string
}

type HTTPVerifier struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	BatchSize  int
	Timeout    time.Duration
	MaxRetries int
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type verificationEnvelope struct {
	Results []VerificationResult `json:"results"`
}

func (verifier *HTTPVerifier) Verify(contextValue context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	batchSize := verifier.BatchSize
	if batchSize <= 0 {
		batchSize = 8
	}
	results := make(map[string]VerificationResult, len(inputs))
	for startingIndex := 0; startingIndex < len(inputs); startingIndex += batchSize {
		endingIndex := startingIndex + batchSize
		if endingIndex > len(inputs) {
			endingIndex = len(inputs)
		}
		batchResults, batchError := verifier.verifyBatchWithFallback(contextValue, inputs[startingIndex:endingIndex])
		if batchError != nil {
			return nil, batchError
		}
		for messageID, result := range batchResults {
			results[messageID] = result
		}
	}
	return results, nil
}

func (verifier *HTTPVerifier) Identity() string {
	return normalize.Hash(
		VerificationPromptVersion,
		inference.NormalizeBaseURL(verifier.BaseURL),
		verifierModel(verifier.Model),
	)
}

func (verifier *HTTPVerifier) verifyBatchWithFallback(contextValue context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	results, verifyError := verifier.verifyBatchWithRetries(contextValue, inputs)
	if verifyError == nil || len(inputs) <= 1 {
		return results, verifyError
	}
	middleIndex := len(inputs) / 2
	leftResults, leftError := verifier.verifyBatchWithFallback(contextValue, inputs[:middleIndex])
	if leftError != nil {
		return nil, leftError
	}
	rightResults, rightError := verifier.verifyBatchWithFallback(contextValue, inputs[middleIndex:])
	if rightError != nil {
		return nil, rightError
	}
	for messageID, result := range rightResults {
		leftResults[messageID] = result
	}
	return leftResults, nil
}

func (verifier *HTTPVerifier) verifyBatchWithRetries(contextValue context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	maximumRetries := verifier.MaxRetries
	if maximumRetries < 0 {
		maximumRetries = 0
	}
	var lastError error
	for attemptNumber := 0; attemptNumber <= maximumRetries; attemptNumber++ {
		results, verifyError := verifier.verifyBatch(contextValue, inputs)
		if verifyError == nil {
			return results, nil
		}
		lastError = verifyError
		if attemptNumber == maximumRetries {
			break
		}
		delay := time.Second * time.Duration(1<<min(attemptNumber, 4))
		timer := time.NewTimer(delay)
		select {
		case <-contextValue.Done():
			timer.Stop()
			return nil, contextValue.Err()
		case <-timer.C:
		}
	}
	return nil, lastError
}

func (verifier *HTTPVerifier) verifyBatch(contextValue context.Context, inputs []VerificationInput) (map[string]VerificationResult, error) {
	encodedInputs, marshalError := json.Marshal(inputs)
	if marshalError != nil {
		return nil, fmt.Errorf("encode verifier inputs: %w", marshalError)
	}

	systemPrompt := `Classify historical user messages. A definition request asks for the meaning, definition, interpretation, usage, pronunciation, translation, or grammatical meaning of a word, phrase, idiom, expression, or technical term. Broad requests to explain a whole topic, event, product, algorithm, or situation are not definition requests unless the wording specifically asks what a term means. Use neighboring messages only to resolve references such as "that". Return one result for every input ID.`
	requestPayload := map[string]any{
		"model": verifierModel(verifier.Model),
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": string(encodedInputs)},
		},
		"temperature": 0,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "definition_request_classification",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"results": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"id":                    map[string]any{"type": "string"},
									"is_definition_request": map[string]any{"type": "boolean"},
									"terms":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									"category":              map[string]any{"type": "string"},
									"confidence":            map[string]any{"type": "number"},
									"explanation":           map[string]any{"type": "string"},
								},
								"required": []string{"id", "is_definition_request", "terms", "category", "confidence", "explanation"},
							},
						},
					},
					"required": []string{"results"},
				},
			},
		},
	}
	encodedRequest, requestMarshalError := json.Marshal(requestPayload)
	if requestMarshalError != nil {
		return nil, fmt.Errorf("encode verifier request: %w", requestMarshalError)
	}

	httpClient := verifier.HTTPClient
	if httpClient == nil {
		timeout := verifier.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Minute
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	endpoint := chatCompletionsEndpoint(verifier.BaseURL)
	request, requestError := http.NewRequestWithContext(contextValue, http.MethodPost, endpoint, bytes.NewReader(encodedRequest))
	if requestError != nil {
		return nil, fmt.Errorf("create verifier request: %w", requestError)
	}
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(verifier.APIKey) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(verifier.APIKey))
	}
	response, responseError := httpClient.Do(request)
	if responseError != nil {
		return nil, fmt.Errorf("send verifier request: %w", responseError)
	}
	responseBody, readError := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	response.Body.Close()
	if readError != nil {
		return nil, fmt.Errorf("read verifier response: %w", readError)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("verifier API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var decodedResponse chatCompletionResponse
	if decodeError := json.Unmarshal(responseBody, &decodedResponse); decodeError != nil {
		return nil, fmt.Errorf("decode verifier response: %w", decodeError)
	}
	if decodedResponse.Error != nil {
		return nil, fmt.Errorf("verifier API error: %s", decodedResponse.Error.Message)
	}
	if len(decodedResponse.Choices) == 0 {
		return nil, fmt.Errorf("verifier API returned no choices")
	}
	var envelope verificationEnvelope
	if contentError := json.Unmarshal([]byte(decodedResponse.Choices[0].Message.Content), &envelope); contentError != nil {
		return nil, fmt.Errorf("decode verifier structured output: %w", contentError)
	}
	results := make(map[string]VerificationResult, len(envelope.Results))
	for _, result := range envelope.Results {
		results[result.MessageID] = result
	}
	for _, input := range inputs {
		if _, exists := results[input.MessageID]; !exists {
			return nil, fmt.Errorf("verifier omitted message ID %s", input.MessageID)
		}
	}
	return results, nil
}

func verifierModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return inference.DefaultVerifierModel
	}
	return strings.TrimSpace(model)
}

func chatCompletionsEndpoint(baseURL string) string {
	endpoint := inference.NormalizeBaseURL(baseURL)
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	return endpoint
}
