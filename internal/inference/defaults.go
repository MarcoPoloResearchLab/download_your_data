package inference

import (
	"net"
	"net/url"
	"strings"
)

const (
	BaseURLEnvironment          = "DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL"
	DefaultBaseURL              = "http://127.0.0.1:1234/v1"
	DefaultEmbeddingProvider    = "lmstudio"
	DefaultEmbeddingModel       = "download-your-data-embedding"
	DefaultEmbeddingModelSource = "text-embedding-nomic-embed-text-v1.5"
	DefaultEmbeddingDimensions  = 768
	DefaultEmbeddingInputPrefix = "classification: "
	DefaultVerifierModel        = "download-your-data-verifier"
)

func ConfiguredBaseURL(environmentValue string) string {
	return NormalizeBaseURL(environmentValue)
}

func NormalizeBaseURL(baseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if normalized == "" {
		return DefaultBaseURL
	}
	return normalized
}

func IsLoopbackBaseURL(baseURL string) bool {
	parsedURL, parseError := url.Parse(NormalizeBaseURL(baseURL))
	if parseError != nil {
		return false
	}
	hostname := strings.TrimSpace(parsedURL.Hostname())
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	address := net.ParseIP(hostname)
	return address != nil && address.IsLoopback()
}
