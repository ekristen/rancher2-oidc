package aggregator

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/aggregator"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/common"
)

func Execute(ctx context.Context, c *cli.Command) error {
	// Parse cache TTL
	cacheTTL := time.Duration(c.Int("cache-ttl")) * time.Minute

	// Create server with options
	server, err := aggregator.NewServer(aggregator.Options{
		BaseURL:    c.String("base-url"),
		Logger:     zap.L(),
		CacheTTL:   cacheTTL,
		Kubeconfig: c.String("kubeconfig"),
	})
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Configure HTTP server
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", c.Int("port")),
		Handler:           server,
		ReadTimeout:       1 * time.Second,
		WriteTimeout:      1 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}

	// Start server
	go func() {
		logger := zap.L().With(
			zap.Int64("port", int64(c.Int("port"))),
			zap.String("base_url", c.String("base-url")),
			zap.Duration("cache_ttl", cacheTTL),
		)
		logger.Info("starting OIDC aggregator server")

		var err error
		if c.String("cert-file") != "" && c.String("key-file") != "" {
			err = srv.ListenAndServeTLS(c.String("cert-file"), c.String("key-file"))
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for interrupt signal
	<-sigCh

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		zap.L().Error("shutdown error", zap.Error(err))
		return err
	}

	return nil
}

func init() {
	flags := []cli.Flag{
		&cli.IntFlag{
			Name:    "port",
			Aliases: []string{"p"},
			Value:   8080,
			Usage:   "HTTP server port",
		},
		&cli.StringFlag{
			Name:     "base-url",
			Required: true,
			Usage:    "Base URL for OIDC endpoints (e.g., https://aggregator.example.com)",
		},
		&cli.StringFlag{
			Name:  "cert-file",
			Usage: "TLS certificate file path",
		},
		&cli.StringFlag{
			Name:  "key-file",
			Usage: "TLS private key file path",
		},
		&cli.IntFlag{
			Name:  "cache-ttl",
			Value: 15,
			Usage: "Cache TTL in minutes for OIDC data fetched from downstream clusters",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Usage:   "Path to kubeconfig file (uses in-cluster config if not specified)",
			Sources: cli.EnvVars("KUBECONFIG"),
		},
	}

	cmd := &cli.Command{
		Name:        "aggregator",
		Usage:       "Run the OIDC aggregator server",
		Description: "Starts the OIDC aggregator service that provides discovery and JWKS endpoints",
		Before: func(ctx context.Context, c *cli.Command) error {
			_, err := common.Before(ctx, c)
			return err
		},
		Flags:  append(common.Flags(), flags...),
		Action: Execute,
	}

	common.RegisterCommand(cmd)
}
