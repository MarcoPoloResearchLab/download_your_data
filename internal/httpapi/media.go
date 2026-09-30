package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/media"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/enrichment"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/primevideo"
)

const (
	primeProviderPath       = "/api/providers/prime-video"
	primeGenerationsPath    = primeProviderPath + "/generations"
	viewingHistoryPath      = "/api/viewing-history"
	primeDeleteConfirmation = "delete-prime-video-provider"
)

var netflixEpisodeSuffix = regexp.MustCompile(`^Season ([1-9][0-9]{0,2}): (.+)$`)

type primeGenerationResponse struct {
	Generation primevideo.Generation `json:"generation"`
}
type primeSelectionRequest struct {
	Datasets     []string `json:"datasets"`
	ProfileLabel string   `json:"profile_label"`
}

func registerMediaRoutes(routes *http.ServeMux, operations *primeOperations, registry *netflixWorkspaceRegistry, logger *slog.Logger) {
	withStore := func(operation func(*primevideo.Store, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasPrefix(request.URL.Path, primeProviderPath) {
				if err := requireQueryKeys(request, nil); err != nil {
					writeRequestError(writer, http.StatusBadRequest, "invalid_query")
					return
				}
			}
			user, err := authentication.UserFromRequest(request)
			if err != nil {
				writeRequestError(writer, http.StatusInternalServerError, "internal_error")
				return
			}
			if err := operations.transaction(request.Context(), user, func(store *primevideo.Store) error { operation(store, writer, request); return nil }); err != nil {
				writeMediaError(writer, logger, err)
			}
		}
	}
	routes.HandleFunc("GET "+primeProviderPath, withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		snapshot := store.Snapshot()
		snapshot.TMDBConfigured = operations.client != nil
		writeJSON(writer, logger, http.StatusOK, snapshot)
	}))
	routes.HandleFunc("POST "+primeGenerationsPath, withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			AnalysisLevel      string `json:"analysis_level"`
			SourceGenerationID string `json:"source_generation_id"`
			Locale             string `json:"locale"`
		}
		if err := decodeJSONRequest(writer, request, &payload); err != nil {
			writeJSONRequestError(writer, err)
			return
		}
		var generation primevideo.Generation
		var err error
		status := http.StatusCreated
		switch payload.AnalysisLevel {
		case "", "local":
			if payload.SourceGenerationID != "" || payload.Locale != "" {
				writeRequestError(writer, http.StatusUnprocessableEntity, "invalid_generation_request")
				return
			}
			generation, err = store.Create(request.Context())
		case "tmdb":
			if operations.client == nil {
				writeRequestError(writer, http.StatusUnprocessableEntity, "tmdb_not_configured")
				return
			}
			locale, localeErr := tmdb.NewLocale(payload.Locale)
			if localeErr != nil {
				writeRequestError(writer, http.StatusUnprocessableEntity, "invalid_locale")
				return
			}
			generation, err = store.BeginEnrichment(request.Context(), payload.SourceGenerationID, locale, enrichment.AuthorizeTMDBTitleQueries())
			if err == nil {
				user, userErr := authentication.UserFromRequest(request)
				if userErr != nil {
					err = userErr
				} else {
					err = operations.start(user, generation)
				}
			}
			status = http.StatusAccepted
		default:
			writeRequestError(writer, http.StatusUnprocessableEntity, "invalid_analysis_level")
			return
		}
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		writer.Header().Set("Location", primeGenerationsPath+"/"+generation.ID)
		writeJSON(writer, logger, status, primeGenerationResponse{Generation: generation})
	}))
	routes.HandleFunc("GET "+primeGenerationsPath+"/{generationID}", withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		generation, err := store.Generation(request.PathValue("generationID"))
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		writeJSON(writer, logger, http.StatusOK, primeGenerationResponse{Generation: generation})
	}))
	routes.HandleFunc("PUT "+primeGenerationsPath+"/{generationID}/archive", withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || contentType != "application/zip" {
			writeRequestError(writer, http.StatusUnsupportedMediaType, "invalid_content_type")
			return
		}
		encoded, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, primevideo.MaxUploadBytes))
		if err != nil {
			writeRequestError(writer, http.StatusRequestEntityTooLarge, "upload_too_large")
			return
		}
		generation, err := store.Upload(request.Context(), request.PathValue("generationID"), encoded)
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		writeJSON(writer, logger, http.StatusOK, primeGenerationResponse{Generation: generation})
	}))
	routes.HandleFunc("PUT "+primeGenerationsPath+"/{generationID}/selection", withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		var payload primeSelectionRequest
		if err := decodeJSONRequest(writer, request, &payload); err != nil {
			writeJSONRequestError(writer, err)
			return
		}
		selection, err := primevideo.NewSelection(payload.Datasets, payload.ProfileLabel)
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		generation, err := store.Select(request.Context(), request.PathValue("generationID"), selection)
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		writeJSON(writer, logger, http.StatusOK, primeGenerationResponse{Generation: generation})
	}))
	routes.HandleFunc("DELETE "+primeGenerationsPath+"/{generationID}", withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		if err := store.DeleteGeneration(request.PathValue("generationID")); err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		user, err := authentication.UserFromRequest(request)
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		operations.cancelGeneration(user, request.PathValue("generationID"))
		writer.WriteHeader(http.StatusNoContent)
	}))
	routes.HandleFunc("DELETE "+primeProviderPath, withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Confirmation string `json:"confirmation"`
		}
		if err := decodeJSONRequest(writer, request, &payload); err != nil {
			writeJSONRequestError(writer, err)
			return
		}
		if payload.Confirmation != primeDeleteConfirmation {
			writeRequestError(writer, http.StatusUnprocessableEntity, "confirmation_required")
			return
		}
		user, err := authentication.UserFromRequest(request)
		if err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		operations.cancelUser(user)
		if err := store.DeleteProvider(); err != nil {
			writeMediaError(writer, logger, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	reportHandler := func(export bool) http.HandlerFunc {
		return withStore(func(store *primevideo.Store, writer http.ResponseWriter, request *http.Request) {
			filter, err := mediaFilter(request)
			if err != nil {
				writeRequestError(writer, http.StatusBadRequest, "invalid_query")
				return
			}
			records, err := store.Records(request.Context())
			if err != nil {
				writeMediaError(writer, logger, err)
				return
			}
			user, err := authentication.UserFromRequest(request)
			if err != nil {
				writeRequestError(writer, http.StatusInternalServerError, "internal_error")
				return
			}
			workspace, release, err := registry.acquire(user)
			if err != nil {
				writeMediaError(writer, logger, err)
				return
			}
			defer release()
			netflixRecords, err := netflixMediaRecords(request, workspace)
			if err != nil {
				writeMediaError(writer, logger, err)
				return
			}
			records = append(records, netflixRecords...)
			if export {
				writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
				writer.Header().Set("Content-Disposition", `attachment; filename="viewing-history.csv"`)
				if err := media.ExportCSV(writer, records, filter); err != nil {
					logger.Error("Media export failed", "error_type", "write_failed")
				}
				return
			}
			limit := 0
			if value := request.URL.Query().Get("limit"); value != "" {
				limit, err = strconv.Atoi(value)
				if err != nil || limit < 1 {
					writeRequestError(writer, http.StatusBadRequest, "invalid_query")
					return
				}
			}
			report, err := media.Build(records, filter, request.URL.Query().Get("cursor"), request.URL.Query().Get("titles_cursor"), limit)
			if err != nil {
				if errors.Is(err, media.ErrStaleCursor) {
					writeRequestError(writer, http.StatusConflict, "stale_cursor")
					return
				}
				writeRequestError(writer, http.StatusBadRequest, "invalid_query")
				return
			}
			writeJSON(writer, logger, http.StatusOK, report)
		})
	}
	routes.HandleFunc("GET "+viewingHistoryPath, reportHandler(false))
	routes.HandleFunc("GET "+viewingHistoryPath+"/export", reportHandler(true))
}

func mediaFilter(request *http.Request) (media.Filter, error) {
	if err := requireQueryKeys(request, []string{"provider", "timezone", "start_date", "end_date", "title", "title_id", "kind", "match_status", "media_type", "limit", "cursor", "titles_cursor"}); err != nil {
		return media.Filter{}, err
	}
	query := request.URL.Query()
	return media.NewFilter(query.Get("provider"), query.Get("timezone"), query.Get("start_date"), query.Get("end_date"), query.Get("title"), query.Get("kind"), query.Get("match_status"), query.Get("title_id"), query.Get("media_type"))
}

func netflixMediaRecords(request *http.Request, workspace *netflixlibrary.Workspace) ([]media.Record, error) {
	result := []media.Record{}
	snapshot := workspace.Snapshot()
	if snapshot.Active == nil {
		return result, nil
	}
	label, err := workspace.ProfileLabel(snapshot.Active.ID)
	if err != nil {
		return nil, err
	}
	cursor := ""
	for {
		page, err := workspace.Records(request.Context(), snapshot.Active.ID, cursor, media.MaxPageSize, netflixlibrary.ActivityFilter{})
		if err != nil {
			return nil, err
		}
		for _, activity := range page.Records {
			value := media.Activity{Provider: media.Netflix, Kind: media.ActivityEntry, Title: activity.RawTitle, RawTitle: activity.RawTitle, SourceDate: activity.RawDate, Date: activity.DateISO, DatePrecision: media.CalendarDate, Completion: "unknown", ContentType: "content", ProfileType: "unknown", SearchTitle: activity.DerivedTitle, TitleIdentity: activity.TitleIdentity, MatchStatus: "not_enriched", Source: media.Source{File: "Viewing activity.csv", Row: int(activity.Index) + 1, GenerationID: snapshot.Active.ID}}
			value.ProfileLabel = label
			suffix, found := strings.CutPrefix(activity.RawTitle, activity.DerivedTitle+": ")
			if found {
				parts := netflixEpisodeSuffix.FindStringSubmatch(suffix)
				if len(parts) > 0 {
					value.SeriesTitle = activity.DerivedTitle
					value.SeasonNumber, _ = strconv.Atoi(parts[1])
					value.EpisodeTitle = parts[2]
				}
			}
			if activity.Match != nil {
				value.MatchStatus = string(activity.Match.Status)
			}
			if activity.Metadata != nil {
				metadata := activity.Metadata
				value.Metadata = &media.Metadata{MediaType: string(metadata.MediaType), TMDBID: metadata.TMDBID, Title: metadata.MatchedTitle, Genres: metadata.Genres, OriginalLanguage: metadata.OriginalLanguage, RuntimeMinutes: metadata.RuntimeMinutes, ReleaseDate: metadata.ReleaseDate, IMDbID: metadata.IMDbID}
			}
			record, err := media.NewRecord(value)
			if err != nil {
				return nil, err
			}
			result = append(result, record)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return result, nil
}

func writeMediaError(writer http.ResponseWriter, logger *slog.Logger, err error) {
	var failure *primevideo.Error
	if errors.As(err, &failure) {
		status := http.StatusUnprocessableEntity
		switch failure.Code {
		case "not_found":
			status = http.StatusNotFound
		case "conflict":
			status = http.StatusConflict
		case "upload_too_large", "limit_exceeded":
			status = http.StatusRequestEntityTooLarge
		case "invalid_persistence":
			status = http.StatusInternalServerError
		case "canceled":
			status = http.StatusRequestTimeout
		}
		writeRequestError(writer, status, failure.Code)
		return
	}
	var netflixFailure *netflixlibrary.Error
	if errors.As(err, &netflixFailure) {
		writeNetflixLibraryError(writer, logger, err)
		return
	}
	logger.Error("Media operation failed", "error_type", "internal_error")
	writeRequestError(writer, http.StatusInternalServerError, "internal_error")
}
