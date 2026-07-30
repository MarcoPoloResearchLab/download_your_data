package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

const (
	healthPath          = "/api/health"
	capabilitiesPath    = "/api/capabilities"
	healthStatusReady   = "ready"
	inferenceNotChecked = "not_checked"
	csrfHeaderName      = "X-CSRF-Token"
)

type healthResponse struct {
	Status string `json:"status"`
}

type capabilitiesResponse struct {
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
	OpenAI  openAIProviderCapability  `json:"openai"`
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
	handler           http.Handler
	workspaceRegistry *netflixWorkspaceRegistry
}

// Handler is the complete application HTTP boundary and owns any resources
// that must be closed when the server stops.
type Handler interface {
	http.Handler
	Close() error
}

// NewHandler constructs the authenticated Download Your Data HTTP boundary.
func NewHandler(
	config runtimeconfig.Config,
	logger *slog.Logger,
) (Handler, error) {
	return newApplicationHandler(config, logger)
}

func (handler *applicationHandler) ServeHTTP(
	responseWriter http.ResponseWriter,
	request *http.Request,
) {
	handler.handler.ServeHTTP(responseWriter, request)
}

func (handler *applicationHandler) Close() error {
	if handler == nil || handler.workspaceRegistry == nil {
		return nil
	}
	return handler.workspaceRegistry.close()
}

func newApplicationHandler(
	config runtimeconfig.Config,
	logger *slog.Logger,
) (*applicationHandler, error) {
	metadataClient, clientError := newNetflixMetadataClient(config)
	if clientError != nil {
		return nil, clientError
	}
	return newApplicationHandlerWithNetflixMetadata(
		config,
		logger,
		metadataClient,
	)
}

func newNetflixMetadataClient(
	config runtimeconfig.Config,
) (enrichment.MetadataClient, error) {
	readToken, configured := config.TMDBReadToken()
	if !configured {
		return nil, nil
	}
	client, clientError := tmdb.NewClient(readToken)
	if clientError != nil {
		return nil, fmt.Errorf("create Netflix TMDB client: %w", clientError)
	}
	return client, nil
}

func newApplicationHandlerWithNetflixMetadata(
	config runtimeconfig.Config,
	logger *slog.Logger,
	metadataClient enrichment.MetadataClient,
) (*applicationHandler, error) {
	if logger == nil {
		return nil, errors.New("create application handler: logger is required")
	}
	if config.CSRFToken() == "" || config.DataRoot().Path() == "" {
		return nil, errors.New("create application handler: runtime configuration is not initialized")
	}
	sessionValidator, validatorError := sessionvalidator.New(
		config.Authentication().SessionValidatorConfig(),
	)
	if validatorError != nil {
		return nil, fmt.Errorf("create TAuth session validator: %w", validatorError)
	}
	authBoundary, boundaryError := authentication.NewBoundary(
		sessionValidator,
		config.Authentication().TenantID(),
	)
	if boundaryError != nil {
		return nil, boundaryError
	}
	staticRoot := frontend.Assets()
	indexDocument, indexError := buildApplicationIndex(config)
	if indexError != nil {
		return nil, indexError
	}
	workspaceRegistry, registryError := newNetflixWorkspaceRegistry(
		config,
		metadataClient,
	)
	if registryError != nil {
		return nil, registryError
	}

	protectedRoutes := http.NewServeMux()
	protectedRoutes.HandleFunc("GET "+capabilitiesPath, writeCapabilities(config, logger))
	registerOpenAIRoutes(protectedRoutes, config, logger)
	registerNetflixRoutes(protectedRoutes, workspaceRegistry, logger)
	protectedRoutes.HandleFunc(
		"DELETE "+userWorkspacePath,
		deleteAuthenticatedWorkspace(workspaceRegistry, logger),
	)
	requestCoordinator := &userRequestCoordinator{}
	routes := http.NewServeMux()
	routes.HandleFunc("GET "+healthPath, writeHealth(logger))
	routes.HandleFunc("GET "+uiConfigPath, writeUIConfig(config, logger))
	routes.Handle(
		"/api/",
		requireAuthenticatedUser(
			authBoundary,
			requestCoordinator,
			protectedRoutes,
			logger,
		),
	)
	routes.HandleFunc("GET /{$}", writeApplicationIndex(indexDocument))
	routes.HandleFunc("GET /index.html", writeApplicationIndex(indexDocument))
	routes.Handle("/", http.FileServer(http.FS(staticRoot)))
	return &applicationHandler{
		handler: applySecurityHeaders(
			config,
			applyRequestBoundary(config, routes),
		),
		workspaceRegistry: workspaceRegistry,
	}, nil
}

func buildApplicationIndex(config runtimeconfig.Config) ([]byte, error) {
	indexSource, readError := fs.ReadFile(frontend.Assets(), "index.html")
	if readError != nil {
		return nil, fmt.Errorf("read embedded application index: %w", readError)
	}
	if bytes.Count(indexSource, []byte(frontend.APIOriginMarker)) != 1 {
		return nil, errors.New("render application index: API origin marker must appear exactly once")
	}
	return bytes.Replace(
		indexSource,
		[]byte(frontend.APIOriginMarker),
		[]byte(html.EscapeString(config.Authentication().APIOrigin())),
		1,
	), nil
}

func writeApplicationIndex(indexDocument []byte) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
		responseWriter.Header().Set("Content-Length", strconv.Itoa(len(indexDocument)))
		if request.Method == http.MethodHead {
			responseWriter.WriteHeader(http.StatusOK)
			return
		}
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write(indexDocument)
	}
}

func writeHealth(logger *slog.Logger) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSON(responseWriter, logger, http.StatusOK, healthResponse{
			Status: healthStatusReady,
		})
	}
}

func writeCapabilities(config runtimeconfig.Config, logger *slog.Logger) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSON(responseWriter, logger, http.StatusOK, capabilitiesResponse{
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
				OpenAI: openAIProviderCapability{
					SemanticSearch: true,
					BrowserUpload:  false,
				},
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

func applySecurityHeaders(config runtimeconfig.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set(
			"Content-Security-Policy",
			buildContentSecurityPolicy(config),
		)
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

func isMutationMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
