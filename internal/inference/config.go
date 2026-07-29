// Package inference owns the validated local-inference identity and URL contract.
package inference

import (
	"fmt"
	"net"
	"net/url"
	pathpkg "path"
	"strconv"
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

// BaseURL is a normalized HTTP or HTTPS inference server boundary.
type BaseURL struct {
	value    string
	loopback bool
}

// NewBaseURL validates and normalizes an inference base URL.
func NewBaseURL(configuredValue string) (BaseURL, error) {
	baseURL := strings.TrimSpace(configuredValue)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if strings.ContainsAny(baseURL, " \t\r\n") {
		return BaseURL{}, fmt.Errorf("validate inference base URL: whitespace is not allowed")
	}
	parsedURL, parseError := url.Parse(baseURL)
	if parseError != nil {
		return BaseURL{}, fmt.Errorf("validate inference base URL: invalid URL syntax")
	}
	if parsedURL.Opaque != "" || parsedURL.Host == "" {
		return BaseURL{}, fmt.Errorf("validate inference base URL: an absolute server URL is required")
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return BaseURL{}, fmt.Errorf("validate inference base URL: scheme must be http or https")
	}
	if parsedURL.User != nil {
		return BaseURL{}, fmt.Errorf("validate inference base URL: credentials are not allowed")
	}
	if parsedURL.RawQuery != "" || parsedURL.ForceQuery {
		return BaseURL{}, fmt.Errorf("validate inference base URL: query strings are not allowed")
	}
	if parsedURL.Fragment != "" {
		return BaseURL{}, fmt.Errorf("validate inference base URL: fragments are not allowed")
	}
	if parsedURL.RawPath != "" || strings.Contains(parsedURL.Path, "\\") {
		return BaseURL{}, fmt.Errorf("validate inference base URL: encoded or backslash paths are not allowed")
	}

	hostname := strings.ToLower(strings.TrimSpace(parsedURL.Hostname()))
	if hostname == "" {
		return BaseURL{}, fmt.Errorf("validate inference base URL: hostname is required")
	}
	port := parsedURL.Port()
	if port != "" {
		portNumber, portError := strconv.Atoi(port)
		if portError != nil || portNumber < 1 || portNumber > 65535 {
			return BaseURL{}, fmt.Errorf("validate inference base URL: port must be between 1 and 65535")
		}
		if (scheme == "http" && portNumber == 80) || (scheme == "https" && portNumber == 443) {
			port = ""
		} else {
			port = strconv.Itoa(portNumber)
		}
	}

	host := hostname
	hostAddress := net.ParseIP(hostname)
	if hostAddress != nil {
		host = hostAddress.String()
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	normalizedPath := ""
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		cleanedPath := pathpkg.Clean(parsedURL.Path)
		if cleanedPath != parsedURL.Path && strings.TrimRight(parsedURL.Path, "/") != cleanedPath {
			return BaseURL{}, fmt.Errorf("validate inference base URL: path must be normalized")
		}
		normalizedPath = strings.TrimRight(cleanedPath, "/")
	}
	normalizedValue := scheme + "://" + host + normalizedPath
	isLoopback := strings.EqualFold(hostname, "localhost") ||
		(hostAddress != nil && hostAddress.IsLoopback())
	return BaseURL{value: normalizedValue, loopback: isLoopback}, nil
}

// String returns the normalized inference base URL.
func (baseURL BaseURL) String() string {
	return baseURL.value
}

// IsLoopback reports whether the URL hostname is local loopback.
func (baseURL BaseURL) IsLoopback() bool {
	return baseURL.loopback
}

// Endpoint returns a normalized relative endpoint beneath the validated base URL.
func (baseURL BaseURL) Endpoint(relativeEndpoint string) (string, error) {
	if baseURL.value == "" {
		return "", fmt.Errorf("build inference endpoint: base URL is not initialized")
	}
	trimmedEndpoint := strings.TrimSpace(relativeEndpoint)
	if trimmedEndpoint == "" ||
		strings.HasPrefix(trimmedEndpoint, "/") ||
		strings.HasSuffix(trimmedEndpoint, "/") ||
		strings.ContainsAny(trimmedEndpoint, `\?#%`) ||
		pathpkg.Clean(trimmedEndpoint) != trimmedEndpoint {
		return "", fmt.Errorf(
			"build inference endpoint %q: a normalized relative path is required",
			relativeEndpoint,
		)
	}
	for _, pathSegment := range strings.Split(trimmedEndpoint, "/") {
		if pathSegment == "" || pathSegment == "." || pathSegment == ".." {
			return "", fmt.Errorf(
				"build inference endpoint %q: a normalized relative path is required",
				relativeEndpoint,
			)
		}
	}
	return baseURL.value + "/" + trimmedEndpoint, nil
}
