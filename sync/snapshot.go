package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/writer"
)

const sidecarWriter = "sync-sidecar"

type SnapshotManifest struct {
	ManifestVersion  int      `json:"manifest_version"`
	Writer           string   `json:"writer"`
	DBName           string   `json:"db_name"`
	SchemaName       string   `json:"schema_name"`
	SnapshotID       string   `json:"snapshot_id"`
	Timestamp        string   `json:"timestamp"`
	TableName        string   `json:"table_name"`
	Query            string   `json:"query"`
	Operation        string   `json:"operation"`
	Actor            *string  `json:"actor"`
	RowCount         int      `json:"row_count"`
	SchemaDDL        string   `json:"schema_ddl"`
	FKConstraints    []string `json:"fk_constraints"`
	Indexes          []string `json:"indexes"`
	CheckConstraints []string `json:"check_constraints"`
	S3Bucket         string   `json:"s3_bucket"`
	S3DataKey        string   `json:"s3_data_key"`
	S3ManifestKey    string   `json:"s3_manifest_key"`
	DataSHA256       string   `json:"data_sha256"`
	SnapshotScope    string   `json:"snapshot_scope"`
	Status           string   `json:"status"`
	ValidationError  string   `json:"validation_error,omitempty"`
	VerifiedAt       string   `json:"verified_at,omitempty"`
	SourceSelectSQL  *string  `json:"source_select_sql"`
}

type SnapshotResult struct {
	Manifest SnapshotManifest
}

type SnapshotEngine struct {
	db           *sql.DB
	s3           *s3.Client
	storage      StorageConfig
	dbName       string
	schema       string
	maxTableRows int
	mu           sync.RWMutex
	latest       map[string]SnapshotManifest
	tableLocks   sync.Map // map[string]*sync.Mutex — per-table capture serialization
}

func NewSnapshotEngine(db *sql.DB, s3Client *s3.Client, storage StorageConfig, dbName string, schema string, maxTableRows int) *SnapshotEngine {
	return &SnapshotEngine{
		db:           db,
		s3:           s3Client,
		storage:      storage,
		dbName:       dbName,
		schema:       schema,
		maxTableRows: maxTableRows,
		latest:       make(map[string]SnapshotManifest),
	}
}

// tableCaptureLock returns the mutex for serializing captures of a specific table.
// Different tables use independent locks so they can be captured concurrently.
func (e *SnapshotEngine) tableCaptureLock(table string) *sync.Mutex {
	v, _ := e.tableLocks.LoadOrStore(table, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// cleanupOrphanedData removes a data object from S3 after a failed snapshot.
// Uses a detached context because the original request context may be cancelled.
func (e *SnapshotEngine) cleanupOrphanedData(dataKey, snapshotID string) {
	cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := e.s3.DeleteObject(cleanCtx, &s3.DeleteObjectInput{
		Bucket: aws.String(e.storage.Bucket),
		Key:    aws.String(dataKey),
	})
	if err != nil {
		slog.Warn("Failed to clean up orphaned snapshot data",
			"key", dataKey, "snapshot_id", snapshotID, "error", err)
	} else {
		slog.Info("Cleaned up orphaned snapshot data",
			"key", dataKey, "snapshot_id", snapshotID)
	}
}

func (e *SnapshotEngine) Latest(table string) (SnapshotManifest, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	manifest, ok := e.latest[table]
	return manifest, ok
}

func (e *SnapshotEngine) VerifyLatest(ctx context.Context, table string) (SnapshotManifest, error) {
	tableMu := e.tableCaptureLock(table)
	tableMu.Lock()
	defer tableMu.Unlock()

	e.mu.RLock()
	manifest, ok := e.latest[table]
	e.mu.RUnlock()
	if !ok {
		return SnapshotManifest{}, fmt.Errorf("no latest snapshot for table %s", table)
	}
	if manifest.Status != "valid" || manifest.DataSHA256 == "" {
		return SnapshotManifest{}, fmt.Errorf("latest snapshot for table %s is not verifiable: status=%q sha256_present=%t", table, manifest.Status, manifest.DataSHA256 != "")
	}
	if err := e.verifyUploadedDataObject(ctx, manifest.S3DataKey, manifest.DataSHA256); err != nil {
		return SnapshotManifest{}, err
	}
	manifest.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	e.mu.Lock()
	e.latest[table] = manifest
	e.mu.Unlock()
	slog.Debug("Snapshot verification refreshed", "table", table, "snapshot_id", manifest.SnapshotID, "verified_at", manifest.VerifiedAt)
	return manifest, nil
}

func (e *SnapshotEngine) CaptureTable(ctx context.Context, table string) (SnapshotManifest, error) {
	// Acquire per-table lock. This serializes captures of the same table while
	// allowing different tables to be captured concurrently. The lock prevents
	// duplicate uploads and race conditions on the latest map if CaptureTable
	// is ever called from multiple goroutines (defense-in-depth).
	tableMu := e.tableCaptureLock(table)
	tableMu.Lock()
	defer tableMu.Unlock()

	snapshotID := generateSnapshotID()
	tableKey := safeTableKey(table)
	baseKey := fmt.Sprintf("%s/snapshots/%s/%s", e.storage.Prefix, tableKey, snapshotID)
	dataKey := baseKey + "/data.parquet"
	manifestKey := baseKey + "/manifest.json"

	slog.Info("Snapshot capture starting",
		"table", table, "snapshot_id", snapshotID, "phase", "schema_capture")

	// Phase 1: Capture schema metadata from PostgreSQL.
	// These are read-only queries against information_schema/pg_catalog.
	schemaDDL, err := e.tableDDL(ctx, table)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("capture schema DDL for %s: %w", table, err)
	}
	fks, err := e.fkConstraints(ctx, table)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("capture FK constraints for %s: %w", table, err)
	}
	checks, err := e.checkConstraints(ctx, table)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("capture CHECK constraints for %s: %w", table, err)
	}
	indexes, err := e.indexes(ctx, table)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("capture indexes for %s: %w", table, err)
	}

	// Phase 2: Write table data to local Parquet temp file and compute SHA256.
	// The Parquet file is the snapshot's data payload; the SHA256 is its integrity seal.
	slog.Info("Snapshot writing parquet",
		"table", table, "snapshot_id", snapshotID, "phase", "parquet_write")

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("backstop-%s.parquet", snapshotID))
	rowCount, err := e.writeTableParquet(ctx, table, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return SnapshotManifest{}, fmt.Errorf("write parquet for %s: %w", table, err)
	}
	defer os.Remove(tmpPath)

	dataHash, err := fileSHA256(tmpPath)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("hash parquet for %s: %w", table, err)
	}
	if strings.TrimSpace(dataHash) == "" {
		return SnapshotManifest{}, fmt.Errorf("computed empty SHA256 for snapshot data of %s", table)
	}

	// Phase 3: Upload data object to S3.
	// S3 PutObject is atomic for single-part uploads — the object is either
	// fully written or not visible. The risk is cross-object inconsistency:
	// data uploaded but manifest not yet written.
	slog.Info("Snapshot uploading data",
		"table", table, "snapshot_id", snapshotID, "rows", rowCount, "phase", "s3_upload")

	file, err := os.Open(tmpPath)
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("open parquet for upload: %w", err)
	}
	defer file.Close()

	_, err = e.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(e.storage.Bucket),
		Key:         aws.String(dataKey),
		Body:        file,
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return SnapshotManifest{}, fmt.Errorf("upload parquet to S3 for %s: %w", table, err)
	}

	// Phase 4: Verify uploaded data integrity by re-reading from S3.
	// This catches S3 transport corruption and ensures the stored object
	// matches the locally computed SHA256.
	slog.Info("Snapshot verifying upload",
		"table", table, "snapshot_id", snapshotID, "phase", "s3_verify")

	if err := e.verifyUploadedDataObject(ctx, dataKey, dataHash); err != nil {
		e.cleanupOrphanedData(dataKey, snapshotID)
		return SnapshotManifest{}, fmt.Errorf("verify uploaded data for %s: %w", table, err)
	}

	// Phase 5: Write manifest to S3.
	// The manifest is the authoritative record of this snapshot. Without a
	// manifest, the data object is invisible to the system (readers scan for
	// manifest.json files). Status is set to "valid" only after data integrity
	// has been verified.
	manifest := SnapshotManifest{
		ManifestVersion:  1,
		Writer:           sidecarWriter,
		DBName:           e.dbName,
		SchemaName:       e.schema,
		SnapshotID:       snapshotID,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		TableName:        table,
		Query:            fmt.Sprintf("SYNC SNAPSHOT %s.%s", e.schema, table),
		Operation:        "SYNC_SNAPSHOT",
		Actor:            nil,
		RowCount:         rowCount,
		SchemaDDL:        schemaDDL,
		FKConstraints:    nonNilStrings(fks),
		CheckConstraints: nonNilStrings(checks),
		Indexes:          nonNilStrings(indexes),
		S3Bucket:         e.storage.Bucket,
		S3DataKey:        dataKey,
		S3ManifestKey:    manifestKey,
		DataSHA256:       dataHash,
		SnapshotScope:    "table",
		Status:           "valid",
		VerifiedAt:       time.Now().UTC().Format(time.RFC3339),
		SourceSelectSQL:  nil,
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		e.cleanupOrphanedData(dataKey, snapshotID)
		return SnapshotManifest{}, fmt.Errorf("marshal manifest for %s: %w", table, err)
	}
	_, err = e.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(e.storage.Bucket),
		Key:         aws.String(manifestKey),
		Body:        bytes.NewReader(raw),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		// Data object uploaded but manifest failed. Clean up the orphaned data
		// so it doesn't accumulate in S3 with no manifest referencing it.
		e.cleanupOrphanedData(dataKey, snapshotID)
		return SnapshotManifest{}, fmt.Errorf("upload manifest to S3 for %s: %w", table, err)
	}

	// Phase 6: Update in-memory latest map. This is the final step; the
	// snapshot is now fully committed to S3 and visible to readers.
	e.mu.Lock()
	e.latest[table] = manifest
	e.mu.Unlock()

	slog.Info("Snapshot complete",
		"table", table, "snapshot_id", snapshotID, "rows", rowCount,
		"manifest", manifestKey, "data_sha256", dataHash, "phase", "complete")
	return manifest, nil
}

func (e *SnapshotEngine) verifyUploadedDataObject(ctx context.Context, key string, wantSHA256 string) error {
	if strings.TrimSpace(wantSHA256) == "" {
		return fmt.Errorf("snapshot data object %s has no expected sha256", key)
	}
	resp, err := e.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.storage.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("verify uploaded snapshot object %s: %w", key, err)
	}
	defer resp.Body.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, resp.Body); err != nil {
		return fmt.Errorf("hash uploaded snapshot object %s: %w", key, err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, wantSHA256) {
		return fmt.Errorf("uploaded snapshot object %s sha256 mismatch: got %s want %s", key, got, wantSHA256)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (e *SnapshotEngine) writeTableParquet(ctx context.Context, table string, path string) (int, error) {
	rows, err := e.db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s.%s", quoteIdent(e.schema), quoteIdent(table)))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	schemaJSON := parquetSchema(cols)
	fw, err := local.NewLocalFileWriter(path)
	if err != nil {
		return 0, err
	}
	defer fw.Close()

	pw, err := writer.NewJSONWriter(schemaJSON, fw, 4)
	if err != nil {
		return 0, err
	}
	defer pw.WriteStop()

	rowCount := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return rowCount, err
		}
		obj := make(map[string]any, len(cols))
		for i, col := range cols {
			obj[col] = normalizeDBValue(values[i])
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return rowCount, err
		}
		if err := pw.Write(string(raw)); err != nil {
			return rowCount, err
		}
		rowCount++
		if e.maxTableRows > 0 && rowCount > e.maxTableRows {
			return rowCount, fmt.Errorf("table %s exceeds max table rows (%d)", table, e.maxTableRows)
		}
	}
	return rowCount, rows.Err()
}

func parquetSchema(columns []string) string {
	fields := make([]map[string]string, 0, len(columns))
	for _, col := range columns {
		fields = append(fields, map[string]string{
			"Tag": fmt.Sprintf("name=%s, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL", col),
		})
	}
	root := map[string]any{
		"Tag":    "name=parquet_go_root",
		"Fields": fields,
	}
	raw, _ := json.Marshal(root)
	return string(raw)
}

func normalizeDBValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (e *SnapshotEngine) tableDDL(ctx context.Context, table string) (string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT column_name, data_type, character_maximum_length, numeric_precision, numeric_scale, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, e.schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var defs []string
	for rows.Next() {
		var name, dataType, nullable string
		var charLen, precision, scale sql.NullInt64
		var def sql.NullString
		if err := rows.Scan(&name, &dataType, &charLen, &precision, &scale, &nullable, &def); err != nil {
			return "", err
		}
		colType := pgColType(dataType, charLen, precision, scale)
		line := fmt.Sprintf("    %s %s", quoteIdent(name), colType)
		if nullable == "NO" {
			line += " NOT NULL"
		}
		if def.Valid {
			line += " DEFAULT " + def.String
		}
		defs = append(defs, line)
	}

	pks, err := e.primaryKeys(ctx, table)
	if err != nil {
		return "", err
	}
	if len(pks) > 0 {
		quoted := make([]string, len(pks))
		for i, pk := range pks {
			quoted[i] = quoteIdent(pk)
		}
		defs = append(defs, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(quoted, ", ")))
	}
	if len(defs) == 0 {
		return fmt.Sprintf("CREATE TABLE %s ()", quoteIdent(table)), nil
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n)", quoteIdent(table), strings.Join(defs, ",\n")), nil
}

func (e *SnapshotEngine) primaryKeys(ctx context.Context, table string) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = $1
		  AND tc.table_name = $2
		ORDER BY kcu.ordinal_position`, e.schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func (e *SnapshotEngine) fkConstraints(ctx context.Context, table string) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT tc.constraint_name, kcu.column_name, ccu.table_name AS foreign_table_name,
		       ccu.column_name AS foreign_column_name, rc.update_rule, rc.delete_rule
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		JOIN information_schema.referential_constraints rc ON tc.constraint_name = rc.constraint_name AND tc.table_schema = rc.constraint_schema
		JOIN information_schema.constraint_column_usage ccu ON rc.unique_constraint_name = ccu.constraint_name AND rc.unique_constraint_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY tc.constraint_name, kcu.ordinal_position`, e.schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, col, foreignTable, foreignCol, updateRule, deleteRule string
		if err := rows.Scan(&name, &col, &foreignTable, &foreignCol, &updateRule, &deleteRule); err != nil {
			return nil, err
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
			quoteIdent(table), quoteIdent(name), quoteIdent(col), quoteIdent(foreignTable), quoteIdent(foreignCol))
		if updateRule != "NO ACTION" {
			stmt += " ON UPDATE " + updateRule
		}
		if deleteRule != "NO ACTION" {
			stmt += " ON DELETE " + deleteRule
		}
		out = append(out, stmt+";")
	}
	return out, rows.Err()
}

func (e *SnapshotEngine) checkConstraints(ctx context.Context, table string) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT con.conname, pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
		WHERE nsp.nspname = $1 AND rel.relname = $2 AND con.contype = 'c'
		ORDER BY con.conname`, e.schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;", quoteIdent(table), quoteIdent(name), def))
	}
	return out, rows.Err()
}

func (e *SnapshotEngine) indexes(ctx context.Context, table string) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
		ORDER BY indexname`, e.schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var indexdef string
		if err := rows.Scan(&indexdef); err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToUpper(indexdef), " PRIMARY KEY ") {
			continue
		}
		out = append(out, indexdef)
	}
	return out, rows.Err()
}

func pgColType(dataType string, charLen, precision, scale sql.NullInt64) string {
	switch dataType {
	case "character varying":
		if charLen.Valid {
			return fmt.Sprintf("VARCHAR(%d)", charLen.Int64)
		}
		return "VARCHAR"
	case "character":
		if charLen.Valid {
			return fmt.Sprintf("CHAR(%d)", charLen.Int64)
		}
		return "CHAR"
	case "numeric":
		if precision.Valid && scale.Valid {
			return fmt.Sprintf("NUMERIC(%d,%d)", precision.Int64, scale.Int64)
		}
		return "NUMERIC"
	case "timestamp with time zone":
		return "TIMESTAMPTZ"
	case "timestamp without time zone":
		return "TIMESTAMP"
	default:
		return strings.ToUpper(dataType)
	}
}

func generateSnapshotID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("snap_%x", time.Now().UnixNano())
	}
	return "snap_" + hex.EncodeToString(b)
}

func safeTableKey(table string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]`)
	return re.ReplaceAllString(table, "_")
}

func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
