package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/frontend"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/uiconfig"
)

const (
	uiConfigPath                    = "/config-ui.yaml"
	userWorkspacePath               = "/api/workspace"
	userWorkspaceDeleteConfirmation = "delete-download-your-data-workspace"
)

var allowedCORSMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
}

type deleteUserWorkspaceRequest struct {
	Confirmation string `json:"confirmation"`
}

func writeUIConfig(
	encodedDocument []byte,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		responseWriter.WriteHeader(http.StatusOK)
		if _, writeError := responseWriter.Write(encodedDocument); writeError != nil {
			logger.Error(
				"write browser configuration",
				"error_type",
				"browser_configuration_write_failed",
			)
		}
	}
}

func buildUIConfig(config runtimeconfig.Config) ([]byte, error) {
	return uiconfig.Render(uiconfig.Input{
		Description:       "Download Your Data",
		PublicOrigin:      config.Authentication().PublicOrigin(),
		TAuthOrigin:       config.Authentication().TAuthURL(),
		GoogleWebClientID: config.Authentication().GoogleClientID(),
		TenantID:          config.Authentication().TenantID(),
		LoginPath:         runtimeconfig.TAuthLoginPath,
		LogoutPath:        runtimeconfig.TAuthLogoutPath,
		NoncePath:         runtimeconfig.TAuthNoncePath,
		SessionPath:       runtimeconfig.TAuthSessionPath,
	})
}

func requireAuthenticatedUser(
	boundary *authentication.Boundary,
	coordinator *userRequestCoordinator,
	next http.Handler,
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		user, authenticationError := boundary.Authenticate(request)
		if authenticationError != nil {
			logger.Info(
				"authentication boundary rejected request",
				"error_type",
				"session_required",
				"method",
				request.Method,
				"path",
				request.URL.EscapedPath(),
			)
			writeRequestError(responseWriter, http.StatusUnauthorized, "session_required")
			return
		}
		authenticatedRequest, injectionError := authentication.WithUser(request, user)
		if injectionError != nil {
			logger.Error(
				"authentication boundary failed",
				"error_type",
				"authenticated_user_injection_failed",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		operation := func() {
			next.ServeHTTP(responseWriter, authenticatedRequest)
		}
		if request.Method == http.MethodDelete && request.URL.Path == userWorkspacePath {
			coordinator.exclusive(user, operation)
			return
		}
		coordinator.shared(user, operation)
	})
}

func deleteAuthenticatedWorkspace(
	registry *netflixWorkspaceRegistry,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload deleteUserWorkspaceRequest
		if decodeError := decodeJSONRequest(responseWriter, request, &payload); decodeError != nil {
			writeJSONRequestError(responseWriter, decodeError)
			return
		}
		if payload.Confirmation != userWorkspaceDeleteConfirmation {
			writeRequestError(
				responseWriter,
				http.StatusUnprocessableEntity,
				"invalid_deletion_confirmation",
			)
			return
		}
		user, userError := authentication.UserFromRequest(request)
		if userError != nil {
			logger.Error(
				"delete user workspace failed",
				"error_type",
				"authenticated_user_unavailable",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		if deleteError := registry.deleteUser(user); deleteError != nil {
			if errors.Is(deleteError, errWorkspaceInUse) {
				writeRequestError(responseWriter, http.StatusConflict, "workspace_in_use")
				return
			}
			logger.Error(
				"delete user workspace failed",
				"error_type",
				"workspace_deletion_failed",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		writeNoContent(responseWriter)
	}
}

func applyRequestBoundary(
	config runtimeconfig.Config,
	next http.Handler,
) http.Handler {
	allowedOrigin := config.Authentication().PublicOrigin()
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin != "" {
			if origin != allowedOrigin {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_origin")
				return
			}
			setCredentialedCORSHeaders(responseWriter, allowedOrigin)
		}
		if request.Method == http.MethodOptions {
			if origin == "" ||
				!validPreflightMethod(request.Header.Get("Access-Control-Request-Method")) ||
				!validPreflightHeaders(request.Header.Get("Access-Control-Request-Headers")) {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_preflight")
				return
			}
			responseWriter.Header().Set(
				"Access-Control-Allow-Methods",
				strings.Join(allowedCORSMethods, ", "),
			)
			responseWriter.Header().Set(
				"Access-Control-Allow-Headers",
				"Content-Type, "+csrfHeaderName,
			)
			responseWriter.Header().Set("Access-Control-Max-Age", "600")
			responseWriter.WriteHeader(http.StatusNoContent)
			return
		}
		if isMutationMethod(request.Method) {
			if origin != allowedOrigin {
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

func setCredentialedCORSHeaders(responseWriter http.ResponseWriter, origin string) {
	responseWriter.Header().Set("Access-Control-Allow-Origin", origin)
	responseWriter.Header().Set("Access-Control-Allow-Credentials", "true")
	responseWriter.Header().Add("Vary", "Origin")
}

func validPreflightMethod(method string) bool {
	return slices.Contains(allowedCORSMethods, method)
}

func validPreflightHeaders(rawHeaders string) bool {
	if strings.TrimSpace(rawHeaders) == "" {
		return true
	}
	for _, rawHeader := range strings.Split(rawHeaders, ",") {
		header := strings.ToLower(strings.TrimSpace(rawHeader))
		if header != "content-type" && header != strings.ToLower(csrfHeaderName) {
			return false
		}
	}
	return true
}

func buildContentSecurityPolicy(config runtimeconfig.Config) string {
	return frontend.ContentSecurityPolicy(
		config.Authentication().APIOrigin(),
		config.Authentication().TAuthURL(),
	)
}
