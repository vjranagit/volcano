package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/vjranagit/volcano/pkg/webhook"
)

var (
	port     = flag.Int("port", 8443, "Webhook server port")
	certFile = flag.String("cert-file", "/etc/webhook/certs/tls.crt", "TLS certificate file")
	keyFile  = flag.String("key-file", "/etc/webhook/certs/tls.key", "TLS private key file")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	logger := setupLogging(*logLevel)
	logger.Info("starting volcano admission webhook",
		"port", *port,
		"cert", *certFile,
		"key", *keyFile,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	server := webhook.NewServer(*port, *certFile, *keyFile, logger)

	if err := server.Run(ctx); err != nil {
		logger.Error("webhook server failed", "error", err)
		os.Exit(1)
	}

	logger.Info("webhook server shutdown complete")
}

func setupLogging(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: logLevel}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}
