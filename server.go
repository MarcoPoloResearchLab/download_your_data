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
	"strconv"
	"strings"
)

const (
	defaultListenAddress     = "127.0.0.1:8787"
	listenAddressEnvironment = "DOWNLOAD_YOUR_DATA_ADDRESS"
	healthPath               = "/api/health"
	healthStatusReady        = "ready"
	contentSecurityPolicy    = "default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self' https://fonts.gstatic.com data:; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' https://cdn.jsdelivr.net https://fonts.googleapis.com"
)

//go:embed index.html app.js data.json images
var applicationAssets embed.FS

type serverConfig struct {
	listenAddress string
}

type healthResponse struct {
	Status    string `json:"status"`
	LocalOnly bool   `json:"local_only"`
}

func loadServerConfig(lookupEnvironment func(string) string) (serverConfig, error) {
	listenAddress := strings.TrimSpace(lookupEnvironment(listenAddressEnvironment))
	if listenAddress == "" {
		listenAddress = defaultListenAddress
	}
	return newServerConfig(listenAddress)
}

func newServerConfig(listenAddress string) (serverConfig, error) {
	host, portText, splitError := net.SplitHostPort(strings.TrimSpace(listenAddress))
	if splitError != nil {
		return serverConfig{}, fmt.Errorf("validate local application address %s: %w", listenAddress, splitError)
	}
	hostAddress := net.ParseIP(host)
	if hostAddress == nil || !hostAddress.IsLoopback() {
		return serverConfig{}, fmt.Errorf("validate local application address %s: host must be a loopback IP address", listenAddress)
	}
	port, portError := strconv.Atoi(portText)
	if portError != nil || port < 1 || port > 65535 {
		return serverConfig{}, fmt.Errorf("validate local application address %s: port must be between 1 and 65535", listenAddress)
	}
	return serverConfig{listenAddress: net.JoinHostPort(hostAddress.String(), strconv.Itoa(port))}, nil
}

func newApplicationHandler(logger *slog.Logger) (http.Handler, error) {
	if logger == nil {
		return nil, errors.New("create application handler: logger is required")
	}
	staticRoot, staticRootError := fs.Sub(applicationAssets, ".")
	if staticRootError != nil {
		return nil, fmt.Errorf("open embedded application assets: %w", staticRootError)
	}

	routes := http.NewServeMux()
	routes.HandleFunc("GET "+healthPath, writeHealth(logger))
	routes.Handle("/", http.FileServer(http.FS(staticRoot)))
	return applySecurityHeaders(routes), nil
}

func writeHealth(logger *slog.Logger) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set("Content-Type", "application/json")
		responseWriter.WriteHeader(http.StatusOK)
		encodeError := json.NewEncoder(responseWriter).Encode(healthResponse{
			Status:    healthStatusReady,
			LocalOnly: true,
		})
		if encodeError != nil {
			logger.Error("write health response", "error", encodeError)
		}
	}
}

func applySecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		responseWriter.Header().Set("Referrer-Policy", "no-referrer")
		responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(responseWriter, request)
	})
}
