package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

func main() {
	targetVersion := flag.Int("target-version", 0, "KMS key version to rekey all rows to (required, 1..32767)")
	batch := flag.Int("batch", 100, "rows per transaction")
	concurrency := flag.Int("concurrency", 4, "parallel batch workers")
	dryRun := flag.Bool("dry-run", false, "report count without writing")
	flag.Parse()

	if *targetVersion < 1 || *targetVersion > 32767 {
		fmt.Fprintf(os.Stderr, "rekey: --target-version must be in [1, 32767]\n")
		os.Exit(2)
	}

	log := logger.New("rekey")
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	handles, err := wire.BootstrapDatabases(ctx, log, cfg)
	if err != nil {
		log.Error("bootstrap", "error", err)
		os.Exit(1)
	}
	defer handles.Close()

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9095"
	}
	metricsSrv := &http.Server{
		Addr:    ":" + metricsPort,
		Handler: promhttp.Handler(),
	}
	go func() {
		if serveErr := metricsSrv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Warn("metrics server error", "error", serveErr)
		}
	}()
	defer func() { _ = metricsSrv.Shutdown(context.Background()) }()

	integrationRepo := repository.NewIntegrationRepository(handles.PG)

	r := NewRekeyer(integrationRepo, handles.Envelope, handles.Enc, handles.PG, int16(*targetVersion), *batch, *concurrency, *dryRun, log)
	if err := r.Run(ctx); err != nil {
		log.Error("rekey run failed", "error", err)
		os.Exit(1)
	}

	if *dryRun {
		return
	}

	remaining, err := integrationRepo.CountRekeyRemaining(ctx, int16(*targetVersion))
	if err != nil {
		log.Error("count remaining", "error", err)
		os.Exit(1)
	}
	if remaining > 0 {
		log.Error("rekey incomplete", "remaining", remaining)
		os.Exit(1)
	}
	log.Info("rekey complete; 0 rows remaining")
}
