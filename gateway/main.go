package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

// generateID returns a random prefixed ID, e.g. "appr_3f9a1b2c".
func generateID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID on rand failure (should never happen).
		return fmt.Sprintf("%s_%x", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

func main() {
	listen := flag.String("listen", ":8080", "TCP address to listen on")
	approvalTimeout := flag.Duration("approval-timeout", 300*time.Second, "How long to wait for human approval before auto-denying")
	alertWebhook := flag.String("alert-webhook", "", "Optional webhook URL for risk alerts")
	dbURL := flag.String("db", "", "Default PostgreSQL connection string used by execute_query and restore command output")
	storage := flag.String("storage", "", "S3 storage URL used for restore command output")
	snapshotBeforeCritical := flag.Bool("snapshot-before-critical", true, "Require a sidecar snapshot_id before executing approved CRITICAL queries")
	auditLog := flag.String("audit-log", os.Getenv("BACKSTOP_GATEWAY_AUDIT_LOG"), "Optional JSONL audit log path for durable gateway audit history")
	authToken := flag.String("auth-token", os.Getenv("BACKSTOP_GATEWAY_AUTH_TOKEN"), "Bearer token required for MCP, approval, pending, audit, metadata, and metrics endpoints")
	authTokensFile := flag.String("auth-tokens-file", os.Getenv("BACKSTOP_AUTH_TOKENS_FILE"), "Optional JSON file defining multiple scoped gateway tokens")
	policyFile := flag.String("policy-file", os.Getenv("BACKSTOP_GATEWAY_POLICY_FILE"), "Optional JSON policy file for SQL safety decisions")
	metadataDB := flag.String("metadata-db", getenvDefault("BACKSTOP_METADATA_DB", "backstop.db"), "SQLite metadata database path")
	allowInsecureNoAuth := flag.Bool("allow-insecure-no-auth", false, "Explicitly allow unauthenticated protected endpoints for local development only")
	requireAuthFlag := flag.Bool("require-auth", true, "Require auth for protected endpoints")
	metricsPublic := flag.Bool("metrics-public", false, "Expose /metrics without authentication")
	environment := flag.String("environment", getenvDefault("BACKSTOP_ENVIRONMENT", "local"), "Environment label recorded in safety metadata, audit, approvals, and alerts")
	clusterID := flag.String("cluster-id", getenvDefault("BACKSTOP_CLUSTER_ID", "local"), "Cluster label recorded in safety metadata, audit, approvals, and alerts")
	pauseFile := flag.String("pause-file", os.Getenv("BACKSTOP_PAUSE_FILE"), "Optional JSON file used to persist emergency pause state")
	startPaused := flag.Bool("start-paused", false, "Start gateway in emergency pause mode")
	pauseBlocksSafe := flag.Bool("pause-blocks-safe", false, "Emergency pause also blocks SAFE read-only queries")
	tlsCert := flag.String("tls-cert", "", "Optional TLS certificate file")
	tlsKey := flag.String("tls-key", "", "Optional TLS private key file")
	flag.Parse()

	if err := validateAuthConfig(*requireAuthFlag, *allowInsecureNoAuth, *authToken, *authTokensFile); err != nil {
		log.Fatalf("backstop-gateway: %v", err)
	}
	auth, err := NewTokenAuthenticator(*authToken, *authTokensFile, !*requireAuthFlag || *allowInsecureNoAuth)
	if err != nil {
		log.Fatalf("backstop-gateway: invalid auth config: %v", err)
	}

	var db *sql.DB
	var verifier SnapshotVerifier
	if *dbURL != "" {
		var err error
		db, err = sql.Open("postgres", *dbURL)
		if err != nil {
			log.Fatalf("backstop-gateway: failed to open database: %v", err)
		}
		defer db.Close()

		pingCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := waitForDB(pingCtx, db); err != nil {
			log.Fatalf("backstop-gateway: failed to connect to database: %v", err)
		}
	}
	if *storage != "" {
		storageConfig, err := ParseStorage(*storage, "", "backstop")
		if err != nil {
			log.Fatalf("backstop-gateway: invalid --storage: %v", err)
		}
		s3Client, err := NewS3Client(context.Background(), storageConfig)
		if err != nil {
			log.Fatalf("backstop-gateway: failed to initialize S3 client: %v", err)
		}
		verifier = NewS3SnapshotVerifier(s3Client, storageConfig)
	}

	policy, err := LoadGatewayPolicy(*policyFile)
	if err != nil {
		log.Fatalf("backstop-gateway: invalid --policy-file: %v", err)
	}
	metadata, err := OpenMetadataStore(*metadataDB)
	if err != nil {
		log.Fatalf("backstop-gateway: failed to open metadata DB: %v", err)
	}
	defer metadata.Close()
	metadata.RecordHealth(context.Background(), "gateway", "starting", map[string]any{"listen": *listen})

	approval := NewApprovalEngine(*approvalTimeout)
	registry := NewAgentRegistryWithStores(*auditLog, metadata)
	mcp := NewMCPServer(approval, registry, *alertWebhook, db, *dbURL, *storage, *snapshotBeforeCritical, verifier)
	mcp.policy = policy
	mcp.environment = *environment
	mcp.clusterID = *clusterID
	pauseController, err := NewPauseController(*pauseFile, *startPaused, *pauseBlocksSafe)
	if err != nil {
		log.Fatalf("backstop-gateway: invalid pause config: %v", err)
	}
	mcp.pause = pauseController
	mcp.metrics.SetPauseState(pauseController.BlocksRisk(RiskHigh))
	alertSink := NewGatewayAlertSink(*alertWebhook, metadata, mcp.metrics)
	mcp.alerts = alertSink

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /", requireScopedAuth(auth, metadata, mcp.metrics, "", makeMCPHandler(mcp)))
	mux.HandleFunc("POST /approve/{id}", requireScopedAuth(auth, metadata, mcp.metrics, "approval:write", makeApproveHandler(approval, true)))
	mux.HandleFunc("POST /deny/{id}", requireScopedAuth(auth, metadata, mcp.metrics, "approval:write", makeApproveHandler(approval, false)))
	mux.HandleFunc("GET /pending", requireScopedAuth(auth, metadata, mcp.metrics, "approval:read", makePendingHandler(approval)))
	mux.HandleFunc("GET /audit", requireScopedAuth(auth, metadata, mcp.metrics, "metadata:read", makeAuditHandler(registry)))
	mux.HandleFunc("GET /metadata/snapshots", requireScopedAuth(auth, metadata, mcp.metrics, "metadata:read", makeMetadataSnapshotsHandler(metadata)))
	mux.HandleFunc("GET /metadata/audit", requireScopedAuth(auth, metadata, mcp.metrics, "metadata:read", makeMetadataAuditHandler(metadata)))
	mux.HandleFunc("GET /metadata/alerts", requireScopedAuth(auth, metadata, mcp.metrics, "metadata:read", makeMetadataAlertsHandler(metadata)))
	mux.HandleFunc("GET /metadata/health", requireScopedAuth(auth, metadata, mcp.metrics, "metadata:read", makeMetadataHealthHandler(metadata)))
	mux.HandleFunc("POST /admin/pause", requireScopedAuth(auth, metadata, mcp.metrics, "admin:*", makePauseHandler(pauseController, alertSink, *environment, *clusterID, true, mcp.metrics)))
	mux.HandleFunc("POST /admin/resume", requireScopedAuth(auth, metadata, mcp.metrics, "admin:*", makePauseHandler(pauseController, alertSink, *environment, *clusterID, false, mcp.metrics)))
	mux.HandleFunc("GET /admin/status", requireScopedAuth(auth, metadata, mcp.metrics, "admin:*", makeAdminStatusHandler(pauseController, *environment, *clusterID)))
	metricsHandler := makeGatewayMetricsHandler(mcp.metrics)
	if *metricsPublic {
		mux.HandleFunc("GET /metrics", metricsHandler)
	} else {
		mux.HandleFunc("GET /metrics", requireScopedAuth(auth, metadata, mcp.metrics, "metrics:read", metricsHandler))
	}

	if !auth.Enabled() {
		log.Println("backstop-gateway: WARNING auth disabled by explicit insecure local mode")
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatalf("backstop-gateway: --tls-cert and --tls-key must be provided together")
	}

	srv := &http.Server{
		Addr:         *listen,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // zero: CRITICAL approval requests may block up to approvalTimeout
		IdleTimeout:  60 * time.Second,
	}

	// Separate listener so we can log the actual address (useful when port is 0).
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("backstop-gateway: failed to listen on %s: %v", *listen, err)
	}
	log.Printf(
		"backstop-gateway: listening on %s (approval-timeout=%s snapshot-before-critical=%t tls=%t audit_log=%t metadata_db=%t)",
		ln.Addr(),
		*approvalTimeout,
		*snapshotBeforeCritical,
		*tlsCert != "",
		*auditLog != "",
		*metadataDB != "",
	)

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if *tlsCert != "" {
			err = srv.ServeTLS(ln, *tlsCert, *tlsKey)
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("backstop-gateway: server error: %v", err)
		}
	}()

	<-quit
	log.Println("backstop-gateway: shutting down…")
	metadata.RecordHealth(context.Background(), "gateway", "stopping", map[string]any{"listen": *listen})

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("backstop-gateway: shutdown error: %v", err)
	}
	log.Println("backstop-gateway: stopped")
}

func getenvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func validateAuthConfig(requireAuth, allowInsecure bool, token, tokenFile string) error {
	if requireAuth && strings.TrimSpace(token) == "" && strings.TrimSpace(tokenFile) == "" && !allowInsecure {
		return fmt.Errorf("auth token is required; set --auth-token/BACKSTOP_GATEWAY_AUTH_TOKEN or --auth-tokens-file/BACKSTOP_AUTH_TOKENS_FILE, or explicitly use --allow-insecure-no-auth=true for local development")
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
			return fmt.Errorf("%w: %v", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

// ---- HTTP handlers ------------------------------------------------------

// handleHealth returns a simple 200 OK JSON body.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "backstop-gateway",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func requireAuth(token string, next http.HandlerFunc) http.HandlerFunc {
	if strings.TrimSpace(token) == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !legacyValidAuthToken(r, token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// makeMCPHandler returns the main JSON-RPC dispatch handler.
func makeMCPHandler(mcp *MCPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
				"error": "Content-Type must be application/json",
			})
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		resp := mcp.Handle(r.Context(), body)

		// CRITICAL approval requests may block; use 200 for all JSON-RPC responses
		// (the error is encoded in the JSON-RPC error field, not HTTP status).
		writeJSON(w, http.StatusOK, resp)
	}
}

// makeApproveHandler handles POST /approve/{id} and POST /deny/{id}.
func makeApproveHandler(engine *ApprovalEngine, approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing approval id"})
			return
		}

		actor := "unknown"
		if principal, ok := principalFromContext(r.Context()); ok {
			actor = principal.Name
		}
		var err error
		if approve {
			err = engine.Approve(id, actor)
		} else {
			err = engine.Deny(id, actor)
		}

		if err != nil {
			status := http.StatusNotFound
			if err != ErrApprovalNotFound {
				status = http.StatusInternalServerError
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}

		action := "approved"
		if !approve {
			action = "denied"
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":      action,
			"approval_id": id,
		})
	}
}

func makePauseHandler(controller *PauseController, alerts *GatewayAlertSink, environment, clusterID string, paused bool, metrics *GatewayMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		reason := strings.TrimSpace(body.Reason)
		if reason == "" {
			if paused {
				reason = "manual emergency pause"
			} else {
				reason = "manual emergency resume"
			}
		}
		if err := controller.SetPaused(paused, reason); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		metrics.SetPauseState(paused)
		eventType := "emergency_resumed"
		if paused {
			eventType = "emergency_paused"
		}
		alerts.Emit(r.Context(), GatewayAlertPayload{
			Severity:          "critical",
			EventType:         eventType,
			Environment:       environment,
			ClusterID:         clusterID,
			RecommendedAction: "Review active agents and pending destructive operations before resuming writes.",
			Message:           reason,
		})
		writeJSON(w, http.StatusOK, map[string]any{"status": eventType, "pause": controller.Status()})
	}
}

func makeAdminStatusHandler(controller *PauseController, environment, clusterID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"environment": environment,
			"cluster_id":  clusterID,
			"pause":       controller.Status(),
		})
	}
}

// makePendingHandler returns a snapshot of all pending approvals.
func makePendingHandler(engine *ApprovalEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"pending": engine.PendingList(),
		})
	}
}

// makeAuditHandler returns the full audit log (optionally filtered by agent_id).
func makeAuditHandler(registry *AgentRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := r.URL.Query().Get("agent_id")
		var entries []AuditEntry
		if agentID != "" {
			entries = registry.GetHistory(agentID)
		} else {
			entries = registry.AllEntries()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"entries": entries,
			"count":   len(entries),
		})
	}
}

func makeGatewayMetricsHandler(metrics *GatewayMetrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metrics.Prometheus()))
	}
}

func makeMetadataSnapshotsHandler(metadata *MetadataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := metadata.QuerySnapshots(r.Context(), r.URL.Query().Get("table"))
		writeMetadataResponse(w, rows, err)
	}
}

func makeMetadataAuditHandler(metadata *MetadataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := metadata.QueryAudit(r.Context(), r.URL.Query().Get("agent_id"), r.URL.Query().Get("risk"))
		writeMetadataResponse(w, rows, err)
	}
}

func makeMetadataAlertsHandler(metadata *MetadataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := metadata.QueryAlerts(r.Context())
		writeMetadataResponse(w, rows, err)
	}
}

func makeMetadataHealthHandler(metadata *MetadataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := metadata.QueryHealth(r.Context())
		writeMetadataResponse(w, rows, err)
	}
}

func writeMetadataResponse(w http.ResponseWriter, rows []map[string]any, err error) {
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

// ---- utility ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("backstop-gateway: json encode error: %v", err)
	}
}

