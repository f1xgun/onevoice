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
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

const defaultConcurrency = 4

func main() {
	os.Exit(run())
}

func run() int {
	targetVersion := flag.Int("target-version", 0, "KMS key version to rekey all rows to (required, 1..32767)")
	batch := flag.Int("batch", 100, "rows per transaction")
	concurrency := flag.Int("concurrency", defaultConcurrency, "parallel batch workers")
	dryRun := flag.Bool("dry-run", false, "report count without writing")
	flag.Parse()

	if *targetVersion < 1 || *targetVersion > 32767 {
		fmt.Fprintf(os.Stderr, "rekey: --target-version must be in [1, 32767]\n")
		return 2
	}

	log := logger.New("rekey")
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load", "error", err)
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	handles, err := wire.BootstrapDatabases(ctx, log, cfg)
	if err != nil {
		log.Error("bootstrap", "error", err)
		return 1
	}
	defer handles.Close()

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9095"
	}
	metricsSrv := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if serveErr := metricsSrv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Warn("metrics server error", "error", serveErr)
		}
	}()
	defer func() { _ = metricsSrv.Shutdown(context.Background()) }()

	integrationRepo := repository.NewIntegrationRepository(handles.PG)

	tv := int16(*targetVersion)
	r := NewRekeyer(integrationRepo, handles.Envelope, handles.Enc, handles.PG, tv, *batch, *concurrency, *dryRun, log)
	if err := r.Run(ctx); err != nil {
		log.Error("rekey run failed", "error", err)
		return 1
	}

	if *dryRun {
		return 0
	}

	remaining, err := integrationRepo.CountRekeyRemaining(ctx, tv)
	if err != nil {
		log.Error("count remaining", "error", err)
		return 1
	}
	if remaining > 0 {
		log.Error("rekey incomplete", "remaining", remaining)
		return 1
	}
	log.Info("rekey complete; 0 rows remaining")
	return 0
}
