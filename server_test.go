package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplicationHTTPContract(testContext *testing.T) {
	handler, handlerError := newApplicationHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	testContext.Run("reports local readiness", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + healthPath)
		if requestError != nil {
			testContext.Fatalf("request health endpoint: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf("health status = %d; want %d", response.StatusCode, http.StatusOK)
		}
		var payload healthResponse
		if decodeError := json.NewDecoder(response.Body).Decode(&payload); decodeError != nil {
			testContext.Fatalf("decode health response: %v", decodeError)
		}
		if payload.Status != healthStatusReady || !payload.LocalOnly {
			testContext.Fatalf("unexpected health response: %+v", payload)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			testContext.Fatalf("health response must not be cached")
		}
	})

	testContext.Run("serves the application shell", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + "/")
		if requestError != nil {
			testContext.Fatalf("request application shell: %v", requestError)
		}
		defer response.Body.Close()
		body, readError := io.ReadAll(response.Body)
		if readError != nil {
			testContext.Fatalf("read application shell: %v", readError)
		}
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf("application status = %d; want %d", response.StatusCode, http.StatusOK)
		}
		if !strings.Contains(string(body), "Download Your Data") {
			testContext.Fatalf("application shell is missing the product title")
		}
		if response.Header.Get("Content-Security-Policy") != contentSecurityPolicy {
			testContext.Fatalf("application response is missing the canonical content security policy")
		}
	})

	testContext.Run("rejects unknown routes", func(testContext *testing.T) {
		response, requestError := http.Get(server.URL + "/not-a-current-route")
		if requestError != nil {
			testContext.Fatalf("request unknown route: %v", requestError)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			testContext.Fatalf("unknown route status = %d; want %d", response.StatusCode, http.StatusNotFound)
		}
	})
}

func TestServerConfigRequiresLoopback(testContext *testing.T) {
	testCases := []struct {
		name          string
		address       string
		wantAddress   string
		wantErrorText string
	}{
		{name: "IPv4 loopback", address: "127.0.0.1:9000", wantAddress: "127.0.0.1:9000"},
		{name: "IPv6 loopback", address: "[::1]:9000", wantAddress: "[::1]:9000"},
		{name: "public bind", address: "0.0.0.0:9000", wantErrorText: "host must be a loopback IP address"},
		{name: "invalid port", address: "127.0.0.1:70000", wantErrorText: "port must be between 1 and 65535"},
		{name: "missing port", address: "127.0.0.1", wantErrorText: "missing port"},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			config, configError := newServerConfig(testCase.address)
			if testCase.wantErrorText != "" {
				if configError == nil || !strings.Contains(configError.Error(), testCase.wantErrorText) {
					testContext.Fatalf("config error = %v; want text %q", configError, testCase.wantErrorText)
				}
				return
			}
			if configError != nil {
				testContext.Fatalf("create server config: %v", configError)
			}
			if config.listenAddress != testCase.wantAddress {
				testContext.Fatalf("listen address = %q; want %q", config.listenAddress, testCase.wantAddress)
			}
		})
	}
}
