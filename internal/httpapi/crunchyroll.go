package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/crunchyroll"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const (
	crunchyrollProviderPath       = "/api/providers/crunchyroll"
	crunchyrollDeleteConfirmation = "delete-crunchyroll-provider"
	ifMatchHeader                 = "If-Match"
	etagHeader                    = "ETag"
)

var crunchyrollRevisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func registerCrunchyrollRoutes(routes *http.ServeMux, config runtimeconfig.Config, logger *slog.Logger) {
	withRoot := func(operation func(privatepath.Root, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if err := requireQueryKeys(request, nil); err != nil {
				writeRequestError(writer, http.StatusBadRequest, "invalid_query")
				return
			}
			user, err := authentication.UserFromRequest(request)
			if err != nil {
				writeCrunchyrollError(writer, logger, err)
				return
			}
			workspace, err := config.UserWorkspace(user)
			if err != nil {
				writeCrunchyrollError(writer, logger, err)
				return
			}
			operation(workspace.Root(), writer, request)
		}
	}
	routes.HandleFunc("GET "+crunchyrollProviderPath, withRoot(func(root privatepath.Root, writer http.ResponseWriter, request *http.Request) {
		archive, err := crunchyroll.Read(request.Context(), root)
		if err != nil {
			writeCrunchyrollError(writer, logger, err)
			return
		}
		writeCrunchyrollSnapshot(writer, logger, archive.Snapshot())
	}))
	routes.HandleFunc("PUT "+crunchyrollProviderPath, withRoot(func(root privatepath.Root, writer http.ResponseWriter, request *http.Request) {
		expected, ok := readCrunchyrollPrecondition(writer, request)
		if !ok {
			return
		}
		contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || contentType != "text/csv" {
			writeRequestError(writer, http.StatusUnsupportedMediaType, "invalid_content_type")
			return
		}
		body := http.MaxBytesReader(writer, request.Body, crunchyroll.MaxUploadBytes)
		defer body.Close()
		archive, err := crunchyroll.Parse(request.Context(), body)
		if err != nil {
			writeCrunchyrollError(writer, logger, err)
			return
		}
		snapshot, err := crunchyroll.Replace(request.Context(), root, expected, archive)
		if err != nil {
			writeCrunchyrollError(writer, logger, err)
			return
		}
		writeCrunchyrollSnapshot(writer, logger, snapshot)
	}))
	routes.HandleFunc("DELETE "+crunchyrollProviderPath, withRoot(func(root privatepath.Root, writer http.ResponseWriter, request *http.Request) {
		expected, ok := readCrunchyrollPrecondition(writer, request)
		if !ok {
			return
		}
		var payload struct {
			Confirmation string `json:"confirmation"`
		}
		if err := decodeJSONRequest(writer, request, &payload); err != nil {
			writeJSONRequestError(writer, err)
			return
		}
		if payload.Confirmation != crunchyrollDeleteConfirmation {
			writeRequestError(writer, http.StatusUnprocessableEntity, "invalid_deletion_confirmation")
			return
		}
		if err := crunchyroll.Delete(request.Context(), root, expected); err != nil {
			writeCrunchyrollError(writer, logger, err)
			return
		}
		writeNoContent(writer)
	}))
}

func readCrunchyrollPrecondition(writer http.ResponseWriter, request *http.Request) (string, bool) {
	header := request.Header.Values(ifMatchHeader)
	if len(header) == 0 {
		writeRequestError(writer, http.StatusPreconditionRequired, "revision_required")
		return "", false
	}
	if len(header) == 1 {
		tag := strings.TrimSpace(header[0])
		if len(tag) > 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
			revision := tag[1 : len(tag)-1]
			if revision == crunchyroll.EmptyRevision || crunchyrollRevisionPattern.MatchString(revision) {
				return revision, true
			}
		}
	}
	writeRequestError(writer, http.StatusBadRequest, "invalid_revision")
	return "", false
}

func writeCrunchyrollSnapshot(writer http.ResponseWriter, logger *slog.Logger, snapshot crunchyroll.Snapshot) {
	writer.Header().Set(etagHeader, strconv.Quote(snapshot.Revision))
	writer.Header().Set("Access-Control-Expose-Headers", etagHeader)
	writeJSON(writer, logger, http.StatusOK, snapshot)
}

func writeCrunchyrollError(writer http.ResponseWriter, logger *slog.Logger, err error) {
	var invalid *crunchyroll.InputError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, crunchyroll.ErrInvalidPersistence):
		logger.Error("Crunchyroll operation failed", "error_type", "invalid_persistence")
		writeRequestError(writer, http.StatusInternalServerError, "invalid_persistence")
	case errors.Is(err, crunchyroll.ErrTooLarge), errors.As(err, &tooLarge):
		writeRequestError(writer, http.StatusRequestEntityTooLarge, "upload_too_large")
	case errors.Is(err, crunchyroll.ErrStaleRevision):
		writeRequestError(writer, http.StatusPreconditionFailed, "stale_revision")
	case errors.Is(err, crunchyroll.ErrBusy):
		writeRequestError(writer, http.StatusConflict, "provider_busy")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeRequestError(writer, http.StatusRequestTimeout, "canceled")
	case errors.As(err, &invalid):
		writeJSON(writer, logger, http.StatusUnprocessableEntity, requestErrorResponse{
			Error: requestErrorPayload{Code: "invalid_crunchyroll_csv", Row: invalid.Row},
		})
	default:
		logger.Error("Crunchyroll operation failed", "error_type", "internal_error")
		writeRequestError(writer, http.StatusInternalServerError, "internal_error")
	}
}
