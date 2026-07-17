package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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
	if runError := run(applicationContext, os.Getenv, logger); runError != nil {
		logger.Error("application stopped", "error", runError)
		os.Exit(1)
	}
}

func run(
	applicationContext context.Context,
	lookupEnvironment func(string) string,
	logger *slog.Logger,
) error {
	config, configError := loadServerConfig(lookupEnvironment)
	if configError != nil {
		return configError
	}
	handler, handlerError := newApplicationHandler(logger)
	if handlerError != nil {
		return handlerError
	}

	listener, listenError := (&net.ListenConfig{}).Listen(
		applicationContext,
		"tcp",
		config.listenAddress,
	)
	if listenError != nil {
		return fmt.Errorf("listen on local application address %s: %w", config.listenAddress, listenError)
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

	logger.Info("local application ready", "url", "http://"+config.listenAddress)
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
