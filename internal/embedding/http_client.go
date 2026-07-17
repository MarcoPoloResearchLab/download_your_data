package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
)

type Embedder interface {
	Embed(context.Context, []string) ([][]float32, error)
}

type HTTPEmbedder struct {
	BaseURL     string
	APIKey      string
	Model       string
	Dimensions  int
	InputPrefix string
	HTTPClient  *http.Client
	MaxRetries  int
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
	Dimensions     int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

func (embedder *HTTPEmbedder) Embed(contextValue context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("embedding request requires at least one input")
	}
	prefixedInputs := make([]string, len(inputs))
	for inputIndex, input := range inputs {
		prefixedInputs[inputIndex] = embedder.InputPrefix + input
	}
	requestPayload := embeddingRequest{
		Model:          embedder.Model,
		Input:          prefixedInputs,
		EncodingFormat: "float",
		Dimensions:     embedder.Dimensions,
	}
	encodedPayload, marshalError := json.Marshal(requestPayload)
	if marshalError != nil {
		return nil, fmt.Errorf("encode embedding request: %w", marshalError)
	}

	httpClient := embedder.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	maximumRetries := embedder.MaxRetries
	if maximumRetries <= 0 {
		maximumRetries = 5
	}

	var lastError error
	for attemptNumber := 0; attemptNumber <= maximumRetries; attemptNumber++ {
		request, requestError := http.NewRequestWithContext(
			contextValue,
			http.MethodPost,
			embeddingsEndpoint(embedder.BaseURL),
			bytes.NewReader(encodedPayload),
		)
		if requestError != nil {
			return nil, fmt.Errorf("create embedding HTTP request: %w", requestError)
		}
		request.Header.Set("Content-Type", "application/json")
		if strings.TrimSpace(embedder.APIKey) != "" {
			request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(embedder.APIKey))
		}

		response, responseError := httpClient.Do(request)
		if responseError != nil {
			lastError = fmt.Errorf("send embedding request: %w", responseError)
			if attemptNumber < maximumRetries {
				if sleepError := sleepWithContext(contextValue, retryDelay(attemptNumber, "")); sleepError != nil {
					return nil, sleepError
				}
				continue
			}
			break
		}

		responseBody, readError := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
		response.Body.Close()
		if readError != nil {
			return nil, fmt.Errorf("read embedding response: %w", readError)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastError = decodeAPIError("embedding", response.StatusCode, responseBody)
			if isRetryableStatus(response.StatusCode) && attemptNumber < maximumRetries {
				if sleepError := sleepWithContext(contextValue, retryDelay(attemptNumber, response.Header.Get("Retry-After"))); sleepError != nil {
					return nil, sleepError
				}
				continue
			}
			break
		}

		var decodedResponse embeddingResponse
		if decodeError := json.Unmarshal(responseBody, &decodedResponse); decodeError != nil {
			return nil, fmt.Errorf("decode embedding response: %w", decodeError)
		}
		if decodedResponse.Error != nil {
			return nil, fmt.Errorf("embedding API error: %s", decodedResponse.Error.Message)
		}
		if len(decodedResponse.Data) != len(inputs) {
			return nil, fmt.Errorf("embedding API returned %d vectors for %d inputs", len(decodedResponse.Data), len(inputs))
		}

		vectors := make([][]float32, len(inputs))
		for _, responseItem := range decodedResponse.Data {
			if responseItem.Index < 0 || responseItem.Index >= len(inputs) {
				return nil, fmt.Errorf("embedding API returned invalid result index %d", responseItem.Index)
			}
			vector := make([]float32, len(responseItem.Embedding))
			for vectorIndex, vectorValue := range responseItem.Embedding {
				vector[vectorIndex] = float32(vectorValue)
			}
			if embedder.Dimensions > 0 && len(vector) != embedder.Dimensions {
				return nil, fmt.Errorf("embedding dimension mismatch: expected %d, received %d", embedder.Dimensions, len(vector))
			}
			normalizeVector(vector)
			vectors[responseItem.Index] = vector
		}
		return vectors, nil
	}

	if lastError == nil {
		lastError = fmt.Errorf("embedding request failed")
	}
	return nil, lastError
}

func embeddingsEndpoint(baseURL string) string {
	trimmedBaseURL := inference.NormalizeBaseURL(baseURL)
	if strings.HasSuffix(trimmedBaseURL, "/embeddings") {
		return trimmedBaseURL
	}
	return trimmedBaseURL + "/embeddings"
}

func normalizeVector(vector []float32) {
	var squaredLength float64
	for _, vectorValue := range vector {
		squaredLength += float64(vectorValue) * float64(vectorValue)
	}
	if squaredLength == 0 {
		return
	}
	inverseLength := float32(1 / math.Sqrt(squaredLength))
	for vectorIndex := range vector {
		vector[vectorIndex] *= inverseLength
	}
}

func decodeAPIError(operation string, statusCode int, responseBody []byte) error {
	var decodedResponse embeddingResponse
	if decodeError := json.Unmarshal(responseBody, &decodedResponse); decodeError == nil && decodedResponse.Error != nil {
		return fmt.Errorf("%s API returned HTTP %d: %s", operation, statusCode, decodedResponse.Error.Message)
	}
	bodyText := strings.TrimSpace(string(responseBody))
	if len(bodyText) > 1000 {
		bodyText = bodyText[:1000] + "..."
	}
	return fmt.Errorf("%s API returned HTTP %d: %s", operation, statusCode, bodyText)
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout || statusCode >= 500
}

func retryDelay(attemptNumber int, retryAfter string) time.Duration {
	if strings.TrimSpace(retryAfter) != "" {
		seconds, parseError := strconv.Atoi(strings.TrimSpace(retryAfter))
		if parseError == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	delay := time.Second * time.Duration(1<<minInteger(attemptNumber, 5))
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func sleepWithContext(contextValue context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-contextValue.Done():
		return contextValue.Err()
	case <-timer.C:
		return nil
	}
}

func minInteger(leftValue int, rightValue int) int {
	if leftValue < rightValue {
		return leftValue
	}
	return rightValue
}
