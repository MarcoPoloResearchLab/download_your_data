package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

const shutdownTimeout = 10 * time.Second

func main() {
	applicationContext, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	homeDirectory, homeError := os.UserHomeDir()
	if homeError != nil {
		logger.Error("application configuration failed", "error_type", "user_home_unavailable")
		os.Exit(1)
	}
	config, configError := runtimeconfig.Load(os.Getenv, homeDirectory, rand.Reader)
	if configError != nil {
		logger.Error("application configuration failed", "error_type", runtimeconfig.Code(configError))
		os.Exit(1)
	}
	if runError := run(applicationContext, config, logger); runError != nil {
		logger.Error("application stopped", "error_type", "application_runtime_failed")
		os.Exit(1)
	}
}

func run(
	applicationContext context.Context,
	config runtimeconfig.Config,
	logger *slog.Logger,
) (runError error) {
	handler, handlerError := newApplicationHandler(config, logger)
	if handlerError != nil {
		return handlerError
	}
	defer func() {
		if closeError := handler.Close(); closeError != nil {
			runError = errors.Join(
				runError,
				fmt.Errorf("close application workspaces: %w", closeError),
			)
		}
	}()

	listener, listenError := (&net.ListenConfig{}).Listen(
		applicationContext,
		"tcp",
		config.ListenAddress(),
	)
	if listenError != nil {
		return fmt.Errorf("listen on local application address %s: %w", config.ListenAddress(), listenError)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	logger.Info("local application ready", "url", "http://"+config.ListenAddress())
	select {
	case serveError := <-serveResult:
		if errors.Is(serveError, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve local application: %w", serveError)
	case <-applicationContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		if shutdownError := server.Shutdown(shutdownContext); shutdownError != nil {
			return fmt.Errorf("shut down local application: %w", shutdownError)
		}
		serveError := <-serveResult
		if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
			return fmt.Errorf("serve local application during shutdown: %w", serveError)
		}
		return nil
	}
}
