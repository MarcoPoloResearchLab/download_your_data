package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const (
	netflixProviderPath       = "/api/providers/netflix"
	netflixGenerationsPath    = "/api/providers/netflix/generations"
	netflixDeleteConfirmation = "delete-netflix-provider"
	netflixTMDBQueryConsent   = "authorize-tmdb-title-queries"
)

type generationResponse struct {
	Generation netflixlibrary.Generation `json:"generation"`
}

type createNetflixGenerationRequest struct {
	AnalysisLevel         netflixlibrary.AnalysisLevel `json:"analysis_level"`
	SourceGenerationID    string                       `json:"source_generation_id,omitempty"`
	Locale                string                       `json:"locale,omitempty"`
	TMDBTitleQueryConsent string                       `json:"tmdb_title_query_consent,omitempty"`
}

type deleteNetflixProviderRequest struct {
	Confirmation string `json:"confirmation"`
}

func registerNetflixRoutes(
	routes *http.ServeMux,
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) {
	routes.HandleFunc(
		"GET "+netflixProviderPath,
		getNetflixProvider(workspace, logger),
	)
	routes.HandleFunc(
		"POST "+netflixGenerationsPath,
		createNetflixGeneration(workspace, logger),
	)
	routes.HandleFunc(
		"PUT "+netflixGenerationsPath+"/{generationID}/viewing-activity",
		uploadNetflixViewingActivity(workspace, logger),
	)
	routes.HandleFunc(
		"GET "+netflixGenerationsPath+"/{generationID}/events",
		getNetflixGenerationEvents(workspace, logger),
	)
	routes.HandleFunc(
		"GET "+netflixGenerationsPath+"/{generationID}/analytics",
		getNetflixGenerationAnalytics(workspace, logger),
	)
	routes.HandleFunc(
		"GET "+netflixGenerationsPath+"/{generationID}/records",
		getNetflixGenerationRecords(workspace, logger),
	)
	routes.HandleFunc(
		"GET "+netflixGenerationsPath+"/{generationID}/export",
		exportNetflixGeneration(workspace, logger),
	)
	routes.HandleFunc(
		"DELETE "+netflixGenerationsPath+"/{generationID}",
		deleteNetflixGeneration(workspace, logger),
	)
	routes.HandleFunc(
		"DELETE "+netflixProviderPath,
		deleteNetflixProvider(workspace, logger),
	)
}

func getNetflixProvider(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, workspace.Snapshot())
	}
}

func createNetflixGeneration(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload createNetflixGenerationRequest
		if decodeError := decodeJSONRequest(responseWriter, request, &payload); decodeError != nil {
			writeJSONRequestError(responseWriter, decodeError)
			return
		}
		var generation netflixlibrary.Generation
		var createError error
		switch payload.AnalysisLevel {
		case netflixlibrary.AnalysisLevelLocal:
			if payload.SourceGenerationID != "" ||
				payload.Locale != "" ||
				payload.TMDBTitleQueryConsent != "" {
				writeRequestError(
					responseWriter,
					http.StatusUnprocessableEntity,
					"invalid_generation_request",
				)
				return
			}
			generation, createError = workspace.CreateLocalGeneration()
		case netflixlibrary.AnalysisLevelTMDB:
			if payload.TMDBTitleQueryConsent != netflixTMDBQueryConsent {
				writeRequestError(
					responseWriter,
					http.StatusUnprocessableEntity,
					string(netflixlibrary.ErrorConsentRequired),
				)
				return
			}
			locale, localeError := tmdb.NewLocale(payload.Locale)
			if localeError != nil {
				writeRequestError(
					responseWriter,
					http.StatusUnprocessableEntity,
					"invalid_locale",
				)
				return
			}
			generation, createError = workspace.CreateTMDBGeneration(
				request.Context(),
				payload.SourceGenerationID,
				locale,
				enrichment.AuthorizeTMDBTitleQueries(),
			)
		default:
			writeRequestError(
				responseWriter,
				http.StatusUnprocessableEntity,
				"invalid_analysis_level",
			)
			return
		}
		if createError != nil {
			writeNetflixLibraryError(responseWriter, logger, createError)
			return
		}
		writeJSON(responseWriter, logger, http.StatusCreated, generationResponse{
			Generation: generation,
		})
	}
}

func exportNetflixGeneration(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		generationID := request.PathValue("generationID")
		records, exportError := workspace.ExportRecords(
			request.Context(),
			generationID,
		)
		if exportError != nil {
			writeNetflixLibraryError(responseWriter, logger, exportError)
			return
		}
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set(
			"Content-Disposition",
			`attachment; filename="netflix-enriched-`+generationID+`.csv"`,
		)
		responseWriter.Header().Set("Content-Type", "text/csv; charset=utf-8")
		if writeError := netflix.WriteEnrichedActivity(
			request.Context(),
			responseWriter,
			records,
		); writeError != nil {
			logger.Error(
				"Netflix export stream failed",
				"error_type",
				"export_write_failed",
				"generation_id",
				generationID,
			)
		}
	}
}

func uploadNetflixViewingActivity(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Encoding") != "" {
			writeRequestError(
				responseWriter,
				http.StatusUnsupportedMediaType,
				"content_encoding_not_allowed",
			)
			return
		}
		if !isViewingActivityContentType(request.Header.Get("Content-Type")) {
			writeRequestError(
				responseWriter,
				http.StatusUnsupportedMediaType,
				"invalid_content_type",
			)
			return
		}
		generation, uploadError := workspace.UploadViewingActivity(
			request.Context(),
			request.PathValue("generationID"),
			request.Body,
		)
		if uploadError != nil {
			writeNetflixLibraryError(responseWriter, logger, uploadError)
			return
		}
		writeJSON(responseWriter, logger, http.StatusAccepted, generationResponse{
			Generation: generation,
		})
	}
}

func getNetflixGenerationEvents(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, []string{"after"}); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		afterSequence := int64(0)
		if rawAfter := request.URL.Query().Get("after"); rawAfter != "" {
			parsedAfter, parseError := strconv.ParseInt(rawAfter, 10, 64)
			if parseError != nil || parsedAfter < 0 {
				writeRequestError(
					responseWriter,
					http.StatusBadRequest,
					"invalid_event_sequence",
				)
				return
			}
			afterSequence = parsedAfter
		}
		events, eventsError := workspace.Events(
			request.PathValue("generationID"),
			afterSequence,
		)
		if eventsError != nil {
			writeNetflixLibraryError(responseWriter, logger, eventsError)
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, events)
	}
}

func getNetflixGenerationAnalytics(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(
			request,
			[]string{"start_date", "end_date"},
		); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		analytics, analyticsError := workspace.Analytics(
			request.Context(),
			request.PathValue("generationID"),
			request.URL.Query().Get("start_date"),
			request.URL.Query().Get("end_date"),
		)
		if analyticsError != nil {
			writeNetflixLibraryError(responseWriter, logger, analyticsError)
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, analytics)
	}
}

func getNetflixGenerationRecords(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(
			request,
			[]string{"cursor", "limit", "match_status"},
		); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		limit := 0
		if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
			parsedLimit, parseError := strconv.Atoi(rawLimit)
			if parseError != nil {
				writeRequestError(
					responseWriter,
					http.StatusBadRequest,
					"invalid_record_limit",
				)
				return
			}
			limit = parsedLimit
		}
		records, recordsError := workspace.Records(
			request.Context(),
			request.PathValue("generationID"),
			request.URL.Query().Get("cursor"),
			limit,
			netflix.MatchStatus(request.URL.Query().Get("match_status")),
		)
		if recordsError != nil {
			writeNetflixLibraryError(responseWriter, logger, recordsError)
			return
		}
		writeJSON(responseWriter, logger, http.StatusOK, records)
	}
}

func deleteNetflixGeneration(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		if deleteError := workspace.DeleteGeneration(
			request.Context(),
			request.PathValue("generationID"),
		); deleteError != nil {
			writeNetflixLibraryError(responseWriter, logger, deleteError)
			return
		}
		writeNoContent(responseWriter)
	}
}

func deleteNetflixProvider(
	workspace *netflixlibrary.Workspace,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload deleteNetflixProviderRequest
		if decodeError := decodeJSONRequest(responseWriter, request, &payload); decodeError != nil {
			writeJSONRequestError(responseWriter, decodeError)
			return
		}
		if payload.Confirmation != netflixDeleteConfirmation {
			writeRequestError(
				responseWriter,
				http.StatusUnprocessableEntity,
				"invalid_deletion_confirmation",
			)
			return
		}
		if deleteError := workspace.DeleteProvider(request.Context()); deleteError != nil {
			writeNetflixLibraryError(responseWriter, logger, deleteError)
			return
		}
		writeNoContent(responseWriter)
	}
}

func writeNetflixLibraryError(
	responseWriter http.ResponseWriter,
	logger *slog.Logger,
	receivedError error,
) {
	var libraryError *netflixlibrary.Error
	if !errors.As(receivedError, &libraryError) {
		logger.Error("Netflix request failed", "error_type", "untyped_netflix_failure")
		writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
		return
	}
	statusCode := http.StatusInternalServerError
	switch libraryError.Code() {
	case netflixlibrary.ErrorInvalidRequest:
		statusCode = http.StatusBadRequest
	case netflixlibrary.ErrorNotFound:
		statusCode = http.StatusNotFound
	case netflixlibrary.ErrorConflict, netflixlibrary.ErrorInvalidState:
		statusCode = http.StatusConflict
	case netflixlibrary.ErrorNotConfigured,
		netflixlibrary.ErrorStaleSource:
		statusCode = http.StatusConflict
	case netflixlibrary.ErrorConsentRequired:
		statusCode = http.StatusUnprocessableEntity
	case netflixlibrary.ErrorRateLimited:
		statusCode = http.StatusTooManyRequests
	case netflixlibrary.ErrorUnavailable:
		statusCode = http.StatusServiceUnavailable
	case netflixlibrary.ErrorInvalidResponse:
		statusCode = http.StatusBadGateway
	case netflixlibrary.ErrorUploadTooLarge:
		statusCode = http.StatusRequestEntityTooLarge
	case netflixlibrary.ErrorInvalidCSV,
		netflixlibrary.ErrorInvalidHeader,
		netflixlibrary.ErrorInvalidRow,
		netflixlibrary.ErrorInvalidTitle,
		netflixlibrary.ErrorInvalidDate,
		netflixlibrary.ErrorLimitExceeded:
		statusCode = http.StatusUnprocessableEntity
	case netflixlibrary.ErrorCanceled:
		statusCode = http.StatusRequestTimeout
	case netflixlibrary.ErrorIncomplete,
		netflixlibrary.ErrorLeaseUnavailable,
		netflixlibrary.ErrorPersistenceFailed,
		netflixlibrary.ErrorInvalidPersistence:
		statusCode = http.StatusInternalServerError
	}
	if statusCode >= 500 {
		logger.Error(
			"Netflix request failed",
			"error_type",
			string(libraryError.Code()),
			"generation_id",
			libraryError.GenerationID(),
		)
	}
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	if encodeError := json.NewEncoder(responseWriter).Encode(requestErrorResponse{
		Error: requestErrorPayload{
			Code:         string(libraryError.Code()),
			GenerationID: libraryError.GenerationID(),
			Row:          libraryError.Row(),
		},
	}); encodeError != nil {
		logger.Error("write JSON response", "error_type", "response_encoding_failed")
	}
}

func decodeJSONRequest(
	responseWriter http.ResponseWriter,
	request *http.Request,
	destination any,
) error {
	if !isJSONContentType(request.Header.Get("Content-Type")) {
		return &jsonRequestError{code: "invalid_content_type"}
	}
	request.Body = http.MaxBytesReader(
		responseWriter,
		request.Body,
		product.MaxNetflixJSONRequestBytes,
	)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(destination); decodeError != nil {
		var maximumBytesError *http.MaxBytesError
		if errors.As(decodeError, &maximumBytesError) {
			return &jsonRequestError{code: "request_too_large"}
		}
		return &jsonRequestError{code: "invalid_json"}
	}
	var trailingValue any
	if trailingError := decoder.Decode(&trailingValue); trailingError == nil {
		return &jsonRequestError{code: "invalid_json"}
	} else if !errors.Is(trailingError, io.EOF) {
		var maximumBytesError *http.MaxBytesError
		if errors.As(trailingError, &maximumBytesError) {
			return &jsonRequestError{code: "request_too_large"}
		}
		return &jsonRequestError{code: "invalid_json"}
	}
	return nil
}

type jsonRequestError struct {
	code string
}

func (requestError *jsonRequestError) Error() string {
	return requestError.code
}

func writeJSONRequestError(
	responseWriter http.ResponseWriter,
	decodeError error,
) {
	var requestError *jsonRequestError
	if !errors.As(decodeError, &requestError) {
		writeRequestError(responseWriter, http.StatusBadRequest, "invalid_json")
		return
	}
	statusCode := http.StatusBadRequest
	if requestError.code == "invalid_content_type" {
		statusCode = http.StatusUnsupportedMediaType
	}
	if requestError.code == "request_too_large" {
		statusCode = http.StatusRequestEntityTooLarge
	}
	writeRequestError(responseWriter, statusCode, requestError.code)
}

func isJSONContentType(value string) bool {
	mediaType, parameters, parseError := mime.ParseMediaType(value)
	if parseError != nil || mediaType != "application/json" {
		return false
	}
	for parameterName, parameterValue := range parameters {
		if parameterName != "charset" ||
			!strings.EqualFold(parameterValue, "utf-8") {
			return false
		}
	}
	return true
}

func isViewingActivityContentType(value string) bool {
	mediaType, parameters, parseError := mime.ParseMediaType(value)
	if parseError != nil || mediaType != "text/csv" {
		return false
	}
	for parameterName, parameterValue := range parameters {
		if parameterName != "charset" ||
			!strings.EqualFold(parameterValue, "utf-8") {
			return false
		}
	}
	return true
}

func requireQueryKeys(request *http.Request, allowedKeys []string) error {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}
	for key, values := range request.URL.Query() {
		if _, exists := allowed[key]; !exists || len(values) != 1 {
			return errors.New("query does not match the current contract")
		}
	}
	return nil
}

func writeNoContent(responseWriter http.ResponseWriter) {
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.WriteHeader(http.StatusNoContent)
}
