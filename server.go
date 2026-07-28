package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const (
	healthPath            = "/api/health"
	capabilitiesPath      = "/api/capabilities"
	healthStatusReady     = "ready"
	inferenceNotChecked   = "not_checked"
	csrfHeaderName        = "X-CSRF-Token"
	contentSecurityPolicy = "default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self' https://fonts.gstatic.com data:; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' https://cdn.jsdelivr.net https://fonts.googleapis.com"
)

//go:embed index.html app.js data.json images
var applicationAssets embed.FS

type healthResponse struct {
	Status    string `json:"status"`
	LocalOnly bool   `json:"local_only"`
}

type capabilitiesResponse struct {
	LocalOnly bool                   `json:"local_only"`
	CSRFToken string                 `json:"csrf_token"`
	DataRoot  dataRootCapability     `json:"data_root"`
	Inference inferenceCapability    `json:"inference"`
	Archive   archiveLimitCapability `json:"archive"`
	Providers providerCapabilities   `json:"providers"`
}

type dataRootCapability struct {
	Ready bool `json:"ready"`
}

type inferenceCapability struct {
	Boundary            runtimeconfig.InferenceBoundary `json:"boundary"`
	Readiness           string                          `json:"readiness"`
	Provider            string                          `json:"provider"`
	EmbeddingModel      string                          `json:"embedding_model"`
	EmbeddingDimensions int                             `json:"embedding_dimensions"`
	VerifierModel       string                          `json:"verifier_model"`
}

type archiveLimitCapability struct {
	BrowserUploadEnabled bool  `json:"browser_upload_enabled"`
	MaxUploadBytes       int64 `json:"max_upload_bytes"`
	MaxConversationBytes int64 `json:"max_conversation_bytes"`
	MaxEntryCount        int   `json:"max_entry_count"`
	MaxCompressionRatio  int   `json:"max_compression_ratio"`
	MaxWorkingBytes      int64 `json:"max_working_bytes"`
	InferenceBatchSize   int   `json:"inference_batch_size"`
}

type providerCapabilities struct {
	Netflix netflixProviderCapability `json:"netflix"`
}

type netflixProviderCapability struct {
	TMDB tmdbCapability `json:"tmdb"`
}

type tmdbCapability struct {
	Configured bool `json:"configured"`
}

type requestErrorResponse struct {
	Error requestErrorPayload `json:"error"`
}

type requestErrorPayload struct {
	Code         string `json:"code"`
	GenerationID string `json:"generation_id,omitempty"`
	Row          int    `json:"row,omitempty"`
}

type applicationHandler struct {
	handler          http.Handler
	netflixWorkspace *netflixlibrary.Workspace
}

func (handler *applicationHandler) ServeHTTP(
	responseWriter http.ResponseWriter,
	request *http.Request,
) {
	handler.handler.ServeHTTP(responseWriter, request)
}

func (handler *applicationHandler) Close() error {
	if handler == nil || handler.netflixWorkspace == nil {
		return nil
	}
	return handler.netflixWorkspace.Close()
}

func newApplicationHandler(
	config runtimeconfig.Config,
	logger *slog.Logger,
) (*applicationHandler, error) {
	if logger == nil {
		return nil, errors.New("create application handler: logger is required")
	}
	if config.CSRFToken() == "" || config.DataRoot().Path() == "" {
		return nil, errors.New("create application handler: runtime configuration is not initialized")
	}
	staticRoot, staticRootError := fs.Sub(applicationAssets, ".")
	if staticRootError != nil {
		return nil, fmt.Errorf("open embedded application assets: %w", staticRootError)
	}
	netflixWorkspace, workspaceError := netflixlibrary.Open(
		config.DataRoot(),
		config.NetflixLibrary(),
		config.NetflixLease(),
		config.NetflixTMDBCache(),
		config.TMDBConfigured(),
	)
	if workspaceError != nil {
		return nil, fmt.Errorf("open Netflix provider workspace: %w", workspaceError)
	}

	routes := http.NewServeMux()
	routes.HandleFunc("GET "+healthPath, writeHealth(logger))
	routes.HandleFunc("GET "+capabilitiesPath, writeCapabilities(config, logger))
	registerNetflixRoutes(routes, netflixWorkspace, logger)
	routes.Handle("/", http.FileServer(http.FS(staticRoot)))
	return &applicationHandler{
		handler:          applySecurityHeaders(applyLocalRequestBoundary(config, routes)),
		netflixWorkspace: netflixWorkspace,
	}, nil
}

func writeHealth(logger *slog.Logger) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSON(responseWriter, logger, http.StatusOK, healthResponse{
			Status:    healthStatusReady,
			LocalOnly: true,
		})
	}
}

func writeCapabilities(config runtimeconfig.Config, logger *slog.Logger) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSON(responseWriter, logger, http.StatusOK, capabilitiesResponse{
			LocalOnly: true,
			CSRFToken: config.CSRFToken(),
			DataRoot: dataRootCapability{
				Ready: true,
			},
			Inference: inferenceCapability{
				Boundary:            config.InferenceBoundary(),
				Readiness:           inferenceNotChecked,
				Provider:            inference.DefaultEmbeddingProvider,
				EmbeddingModel:      inference.DefaultEmbeddingModel,
				EmbeddingDimensions: inference.DefaultEmbeddingDimensions,
				VerifierModel:       inference.DefaultVerifierModel,
			},
			Archive: archiveLimitCapability{
				BrowserUploadEnabled: false,
				MaxUploadBytes:       product.MaxArchiveUploadBytes,
				MaxConversationBytes: product.MaxConversationEntryBytes,
				MaxEntryCount:        product.MaxArchiveEntryCount,
				MaxCompressionRatio:  product.MaxArchiveCompressionRatio,
				MaxWorkingBytes:      product.MaxArchiveWorkingBytes,
				InferenceBatchSize:   product.DefaultInferenceBatchSize,
			},
			Providers: providerCapabilities{
				Netflix: netflixProviderCapability{
					TMDB: tmdbCapability{
						Configured: config.TMDBConfigured(),
					},
				},
			},
		})
	}
}

func writeJSON(
	responseWriter http.ResponseWriter,
	logger *slog.Logger,
	statusCode int,
	payload any,
) {
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	if encodeError := json.NewEncoder(responseWriter).Encode(payload); encodeError != nil {
		logger.Error("write JSON response", "error_type", "response_encoding_failed")
	}
}

func applyLocalRequestBoundary(config runtimeconfig.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		secureRequest := request.TLS != nil
		requestAuthority, hostValid := canonicalLoopbackAuthority(request.Host, secureRequest)
		if !hostValid {
			writeRequestError(responseWriter, http.StatusForbidden, "invalid_host")
			return
		}
		if originValue := strings.TrimSpace(request.Header.Get("Origin")); originValue != "" {
			if !validSameOrigin(originValue, requestAuthority, secureRequest) {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_origin")
				return
			}
		}
		if isMutationMethod(request.Method) {
			originValue := strings.TrimSpace(request.Header.Get("Origin"))
			if originValue == "" || !validSameOrigin(originValue, requestAuthority, secureRequest) {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_origin")
				return
			}
			if request.Header.Get(csrfHeaderName) != config.CSRFToken() {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_csrf_token")
				return
			}
		}
		next.ServeHTTP(responseWriter, request)
	})
}

func applySecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		responseWriter.Header().Set("Referrer-Policy", "no-referrer")
		responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(responseWriter, request)
	})
}

func writeRequestError(responseWriter http.ResponseWriter, statusCode int, code string) {
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	_ = json.NewEncoder(responseWriter).Encode(requestErrorResponse{
		Error: requestErrorPayload{Code: code},
	})
}

func canonicalLoopbackAuthority(authority string, secureRequest bool) (string, bool) {
	trimmedAuthority := strings.TrimSpace(authority)
	if trimmedAuthority == "" ||
		strings.HasSuffix(trimmedAuthority, ":") ||
		strings.ContainsAny(trimmedAuthority, "/@?#") {
		return "", false
	}
	parsedURL, parseError := url.Parse("http://" + trimmedAuthority)
	if parseError != nil || parsedURL.Host == "" || parsedURL.User != nil {
		return "", false
	}
	hostname := strings.ToLower(strings.TrimSpace(parsedURL.Hostname()))
	port := parsedURL.Port()
	if port != "" {
		portNumber, portError := strconv.Atoi(port)
		if portError != nil || portNumber < 1 || portNumber > 65535 {
			return "", false
		}
		defaultPort := 80
		if secureRequest {
			defaultPort = 443
		}
		if portNumber == defaultPort {
			port = ""
		} else {
			port = strconv.Itoa(portNumber)
		}
	}
	if strings.EqualFold(hostname, "localhost") {
		if port == "" {
			return "localhost", true
		}
		return net.JoinHostPort("localhost", port), true
	}
	hostAddress := net.ParseIP(hostname)
	if hostAddress == nil || !hostAddress.IsLoopback() {
		return "", false
	}
	normalizedHost := hostAddress.String()
	if port == "" {
		if strings.Contains(normalizedHost, ":") {
			return "[" + normalizedHost + "]", true
		}
		return normalizedHost, true
	}
	return net.JoinHostPort(normalizedHost, port), true
}

func validSameOrigin(originValue string, requestAuthority string, secureRequest bool) bool {
	originURL, parseError := url.Parse(originValue)
	if parseError != nil ||
		originURL.User != nil ||
		originURL.RawQuery != "" ||
		originURL.Fragment != "" ||
		(originURL.Path != "" && originURL.Path != "/") {
		return false
	}
	expectedScheme := "http"
	if secureRequest {
		expectedScheme = "https"
	}
	if !strings.EqualFold(originURL.Scheme, expectedScheme) {
		return false
	}
	originAuthority, originValid := canonicalLoopbackAuthority(originURL.Host, secureRequest)
	return originValid && originAuthority == requestAuthority
}

func isMutationMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
