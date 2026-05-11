package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--healthcheck" {
		if err := runHTTPHealthcheck(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// -------------------------------------------------------------------------
	// CLI flags
	// -------------------------------------------------------------------------
	dbURL := flag.String("db", "", "PostgreSQL connection string (required). E.g. postgres://user:pass@host/db")
	storage := flag.String("storage", "", "S3 storage URL, e.g. s3://bucket or s3://bucket@http://localhost:9000 for MinIO")
	intervalSec := flag.Int("interval", 60, "Poll interval in seconds (default 60)")
	alertWebhook := flag.String("alert-webhook", "", "Slack / webhook URL to POST drop alerts to (optional)")
	prefix := flag.String("prefix", "backstop", "S3 key prefix for stored snapshots")
	endpointURL := flag.String("endpoint-url", "", "Custom S3 endpoint URL for MinIO or compatible stores")
	schema := flag.String("schema", "public", "PostgreSQL schema to snapshot and monitor")
	snapshotOnStart := flag.Bool("snapshot-on-start", true, "Snapshot every discovered table once on startup")
	snapshotEveryPoll := flag.Bool("snapshot-every-poll", false, "Snapshot every table on every poll instead of only new, changed, or retry-needed tables")
	snapshotTables := flag.String("snapshot-tables", "", "Optional comma-separated allowlist of tables to snapshot and monitor in the selected schema")
	snapshotStartupGrace := flag.Duration("snapshot-startup-grace", 15*time.Second, "Delay before first snapshot poll so database initialization can settle")
	snapshotStablePolls := flag.Int("snapshot-stable-polls", 2, "Consecutive unchanged table fingerprints required before snapshotting a new or changed table")
	snapshotPauseFile := flag.String("snapshot-pause-file", os.Getenv("BACKSTOP_SNAPSHOT_PAUSE_FILE"), "If this file exists, sync records health but skips snapshot capture")
	maxTableRows := flag.Int("max-table-rows", 1000000, "Maximum rows to snapshot per table before failing that table snapshot")
	metadataDB := flag.String("metadata-db", os.Getenv("BACKSTOP_METADATA_DB"), "Optional SQLite metadata database path")
	metricsListen := flag.String("metrics-listen", "", "Optional HTTP listen address for Prometheus metrics, e.g. :9091")
	maxSnapshotAge := flag.Duration("max-snapshot-age", 0, "Alert if latest table snapshot is older than this duration; 0 disables staleness alerts")
	maxSnapshotFailures := flag.Int("max-snapshot-failures", 3, "Alert after this many consecutive snapshot failures per table")
	bypassDetection := flag.Bool("bypass-detection", false, "Poll pg_stat_activity and alert when agent-like roles bypass the gateway")
	allowedApplications := flag.String("allowed-application-names", "", "Comma-separated application_name allowlist for bypass detection")
	allowedRoles := flag.String("allowed-roles", "", "Comma-separated role allowlist for bypass detection")
	allowedClientAddresses := flag.String("allowed-client-addresses", "", "Comma-separated client address/CIDR allowlist for bypass detection")
	agentRoles := flag.String("agent-roles", "", "Comma-separated database roles considered agent-like for bypass detection")
	gatewayApplicationName := flag.String("gateway-application-name", "backstop-gateway", "Expected gateway application_name")
	mode := flag.String("mode", "snapshot-and-detect", "Sidecar mode. Phase 2 supports snapshot-and-detect")
	flag.Parse()

	// -------------------------------------------------------------------------
	// Structured logger (JSON to stdout in production style)
	// -------------------------------------------------------------------------
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	slog.Info("backstop-sync starting",
		"storage", *storage,
		"prefix", *prefix,
		"endpoint_url", *endpointURL,
		"interval_sec", *intervalSec,
		"schema", *schema,
		"snapshot_on_start", *snapshotOnStart,
		"snapshot_every_poll", *snapshotEveryPoll,
		"snapshot_tables", *snapshotTables,
		"snapshot_startup_grace", snapshotStartupGrace.String(),
		"snapshot_stable_polls", *snapshotStablePolls,
		"snapshot_pause_file", *snapshotPauseFile,
		"max_table_rows", *maxTableRows,
		"metadata_db", *metadataDB != "",
		"metrics_listen", *metricsListen,
		"max_snapshot_age", maxSnapshotAge.String(),
		"mode", *mode,
	)

	// -------------------------------------------------------------------------
	// Validate required flags
	// -------------------------------------------------------------------------
	if *dbURL == "" {
		slog.Error("--db flag is required")
		flag.Usage()
		os.Exit(1)
	}
	if *storage == "" {
		slog.Error("--storage flag is required")
		flag.Usage()
		os.Exit(1)
	}
	if *mode != "snapshot-and-detect" {
		slog.Error("unsupported --mode value", "mode", *mode, "supported", "snapshot-and-detect")
		os.Exit(1)
	}

	// -------------------------------------------------------------------------
	// Open DB connection (does not actually connect until first use)
	// -------------------------------------------------------------------------
	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		slog.Error("Failed to open database connection", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Verify connectivity with a short deadline.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer pingCancel()
	if err := waitForDB(pingCtx, db); err != nil {
		slog.Error("Cannot reach PostgreSQL", "error", err)
		os.Exit(1)
	}
	slog.Info("Database connection established")

	var dbName string
	if err := db.QueryRowContext(pingCtx, "SELECT current_database()").Scan(&dbName); err != nil {
		slog.Error("Failed to resolve current database name", "error", err)
		os.Exit(1)
	}

	storageConfig, err := ParseStorage(*storage, *endpointURL, *prefix)
	if err != nil {
		slog.Error("Invalid --storage value", "error", err)
		os.Exit(1)
	}

	s3Client, err := NewS3Client(pingCtx, storageConfig)
	if err != nil {
		slog.Error("Failed to initialize S3 client", "error", err)
		os.Exit(1)
	}

	// -------------------------------------------------------------------------
	// Wire up components
	// -------------------------------------------------------------------------
	metadata, err := OpenMetadataStore(*metadataDB)
	if err != nil {
		slog.Error("Failed to open metadata DB", "error", err)
		os.Exit(1)
	}
	defer metadata.Close()
	if err := metadata.RecordHealth(context.Background(), "sync", "starting", map[string]any{"schema": *schema}); err != nil {
		slog.Error("Failed to record sync startup metadata", "error", err)
		os.Exit(1)
	}

	tracker := NewDeltaTracker()
	metrics := NewSyncMetrics()
	alerter := NewAlertEngineWithMetadata(*alertWebhook, metadata)
	snapshotEngine := NewSnapshotEngine(db, s3Client, storageConfig, dbName, *schema, *maxTableRows)
	poller := NewPoller(
		db,
		*schema,
		time.Duration(*intervalSec)*time.Second,
		tracker,
		alerter,
		snapshotEngine,
		*snapshotOnStart,
	)
	poller.ConfigureSnapshotStrategy(*snapshotEveryPoll)
	poller.ConfigureTableAllowlist(parseCSV(*snapshotTables))
	poller.ConfigureStability(*snapshotStartupGrace, *snapshotStablePolls, *snapshotPauseFile)
	poller.ConfigureLaunchReadiness(metrics, metadata, *maxSnapshotAge, *maxSnapshotFailures)
	if *bypassDetection {
		poller.ConfigureBypassDetector(NewBypassDetector(db, BypassConfig{
			AllowedApplicationNames: parseCSV(*allowedApplications),
			AllowedRoles:            parseCSV(*allowedRoles),
			AllowedClientAddresses:  parseCSV(*allowedClientAddresses),
			AgentRoles:              parseCSV(*agentRoles),
			GatewayApplicationName:  *gatewayApplicationName,
		}))
	}

	// -------------------------------------------------------------------------
	// Graceful shutdown on SIGINT / SIGTERM
	// -------------------------------------------------------------------------
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *metricsListen != "" {
		startMetricsServer(ctx, *metricsListen, metrics)
	}

	// -------------------------------------------------------------------------
	// Run — blocks until context is cancelled
	// -------------------------------------------------------------------------
	poller.Run(ctx)

	if err := metadata.RecordHealth(context.Background(), "sync", "stopped", map[string]any{"schema": *schema}); err != nil {
		slog.Error("Failed to record sync stopped metadata", "error", err)
	}
	slog.Info("backstop-sync shut down cleanly")
}

func startMetricsServer(ctx context.Context, listen string, metrics *SyncMetrics) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		health := metrics.Health()
		w.Header().Set("Content-Type", "application/json")
		if health.Status != "healthy" {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(healthJSON(health))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metrics.Prometheus(time.Now().UTC())))
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("sync metrics server listening", "listen", listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("sync metrics server failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
}

func runHTTPHealthcheck(url string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthcheck failed: %s", resp.Status)
	}
	return nil
}

func waitForDB(ctx context.Context, db *sql.DB) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		lastErr = db.PingContext(pingCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return lastErr
		case <-ticker.C:
		}
	}
}
