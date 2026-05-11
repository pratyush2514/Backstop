package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const sidecarWriter = "sync-sidecar"

type SnapshotManifest struct {
	ManifestVersion int    `json:"manifest_version"`
	Writer          string `json:"writer"`
	DBName          string `json:"db_name"`
	SchemaName      string `json:"schema_name"`
	SnapshotID      string `json:"snapshot_id"`
	Timestamp       string `json:"timestamp"`
	TableName       string `json:"table_name"`
	Operation       string `json:"operation"`
	RowCount        int    `json:"row_count"`
	S3DataKey       string `json:"s3_data_key"`
	S3ManifestKey   string `json:"s3_manifest_key"`
	SnapshotScope   string `json:"snapshot_scope"`
	DataSHA256      string `json:"data_sha256"`
	Status          string `json:"status"`
	ValidationError string `json:"validation_error,omitempty"`
}

type SnapshotVerifier interface {
	VerifyLatestSidecarSnapshot(ctx context.Context, table string, snapshotID string) (SnapshotManifest, error)
}

type S3SnapshotVerifier struct {
	client   *s3.Client
	storage  StorageConfig
	metadata *MetadataStore
}

func (v *S3SnapshotVerifier) LatestSidecarSnapshot(ctx context.Context, table string) (SnapshotManifest, error) {
	latest, err := v.latestSidecarManifest(ctx, table)
	if err != nil {
		return SnapshotManifest{}, err
	}
	if err := v.verifyDataObject(ctx, latest); err != nil {
		return SnapshotManifest{}, err
	}
	return latest, nil
}

func NewS3SnapshotVerifier(client *s3.Client, storage StorageConfig) *S3SnapshotVerifier {
	return &S3SnapshotVerifier{client: client, storage: storage}
}

func (v *S3SnapshotVerifier) SetMetadataStore(metadata *MetadataStore) {
	v.metadata = metadata
}

func (v *S3SnapshotVerifier) VerifyLatestSidecarSnapshot(ctx context.Context, table string, snapshotID string) (SnapshotManifest, error) {
	if strings.TrimSpace(snapshotID) == "" {
		return SnapshotManifest{}, fmt.Errorf("snapshot_id is required")
	}

	latest, err := v.latestSidecarManifest(ctx, table)
	if err != nil {
		return SnapshotManifest{}, err
	}
	if latest.SnapshotID != snapshotID {
		return SnapshotManifest{}, fmt.Errorf("snapshot_id %s is not latest sidecar snapshot for table %s; latest is %s", snapshotID, table, latest.SnapshotID)
	}
	if err := v.verifyDataObject(ctx, latest); err != nil {
		return SnapshotManifest{}, err
	}
	return latest, nil
}

func (v *S3SnapshotVerifier) verifyDataObject(ctx context.Context, manifest SnapshotManifest) error {
	if err := validateSidecarManifest(manifest, manifest.TableName); err != nil {
		return err
	}
	key := strings.TrimSpace(manifest.S3DataKey)
	if key == "" && strings.HasSuffix(manifest.S3ManifestKey, "/manifest.json") {
		key = strings.TrimSuffix(manifest.S3ManifestKey, "/manifest.json") + "/data.parquet"
	}
	if key == "" {
		return fmt.Errorf("snapshot %s does not identify a data object", manifest.SnapshotID)
	}
	resp, err := v.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(v.storage.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("snapshot data object verification failed for %s: %w", key, err)
	}
	defer resp.Body.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, resp.Body); err != nil {
		return fmt.Errorf("snapshot data object hashing failed for %s: %w", key, err)
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(got, manifest.DataSHA256) {
		return fmt.Errorf("snapshot %s data sha256 mismatch: got %s want %s", manifest.SnapshotID, got, manifest.DataSHA256)
	}
	return nil
}

func validateSidecarManifest(manifest SnapshotManifest, table string) error {
	if manifest.SnapshotID == "" {
		return fmt.Errorf("snapshot manifest is missing snapshot_id")
	}
	if manifest.TableName != table {
		return fmt.Errorf("snapshot table mismatch: manifest has %s, query targets %s", manifest.TableName, table)
	}
	if manifest.Writer != sidecarWriter {
		return fmt.Errorf("snapshot %s was written by %s, want %s", manifest.SnapshotID, manifest.Writer, sidecarWriter)
	}
	if manifest.SnapshotScope != "table" {
		return fmt.Errorf("snapshot %s has scope %s, want table", manifest.SnapshotID, manifest.SnapshotScope)
	}
	// Status must be explicitly "valid". Empty or missing status means the
	// snapshot did not complete its integrity verification lifecycle. Any
	// value other than "valid" (e.g. "pending", "incomplete", "invalid")
	// indicates the snapshot is not a trustworthy recovery point.
	status := strings.TrimSpace(manifest.Status)
	if status != "valid" {
		if manifest.ValidationError != "" {
			return fmt.Errorf("snapshot %s is not valid: status=%s validation_error=%s", manifest.SnapshotID, status, manifest.ValidationError)
		}
		if status == "" {
			return fmt.Errorf("snapshot %s has no status field (expected \"valid\")", manifest.SnapshotID)
		}
		return fmt.Errorf("snapshot %s is not valid: status=%s", manifest.SnapshotID, status)
	}
	if strings.TrimSpace(manifest.DataSHA256) == "" {
		return fmt.Errorf("snapshot %s is missing data_sha256", manifest.SnapshotID)
	}
	return nil
}

func (v *S3SnapshotVerifier) latestSidecarManifest(ctx context.Context, table string) (SnapshotManifest, error) {
	prefix := fmt.Sprintf("%s/snapshots/%s/", v.storage.Prefix, safeTableKey(table))
	paginator := s3.NewListObjectsV2Paginator(v.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(v.storage.Bucket),
		Prefix: aws.String(prefix),
	})

	var latest SnapshotManifest
	var latestTime time.Time
	found := false
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return SnapshotManifest{}, err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, "/manifest.json") {
				continue
			}
			manifest, err := v.getManifest(ctx, key)
			if err != nil {
				v.quarantineManifest(ctx, key, table, "", fmt.Sprintf("manifest read/decode failed: %v", err))
				continue
			}
			if manifest.Writer != sidecarWriter || manifest.TableName != table {
				continue
			}
			if err := validateSidecarManifest(manifest, table); err != nil {
				v.quarantineManifest(ctx, key, table, manifest.SnapshotID, err.Error())
				return SnapshotManifest{}, err
			}
			ts, err := parseManifestTime(manifest.Timestamp)
			if err != nil {
				v.quarantineManifest(ctx, key, table, manifest.SnapshotID, fmt.Sprintf("manifest timestamp invalid: %v", err))
				continue
			}
			if !found || ts.After(latestTime) {
				latest = manifest
				latestTime = ts
				found = true
			}
		}
	}
	if !found {
		return SnapshotManifest{}, fmt.Errorf("no sidecar recovery point found for table %s", table)
	}
	return latest, nil
}

func (v *S3SnapshotVerifier) quarantineManifest(ctx context.Context, key, table, snapshotID, reason string) {
	if v == nil || v.metadata == nil {
		return
	}
	if snapshotID == "" {
		snapshotID = snapshotIDFromManifestKey(key)
	}
	if err := v.metadata.QuarantineManifest(ctx, snapshotID, table, key, reason); err != nil {
		// Quarantine write failure is intentionally not hidden from operators:
		// readiness remains failed through the caller's error path, while this
		// alert preserves the audit gap in metadata when possible.
		_ = v.metadata.RecordAlert(ctx, "critical", "manifest_quarantine_failed", table, "failed", map[string]any{
			"manifest_key": key,
			"snapshot_id":  snapshotID,
			"reason":       reason,
			"error":        err.Error(),
		})
	}
}

func snapshotIDFromManifestKey(key string) string {
	parts := strings.Split(strings.Trim(key, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], "snap_") {
			return parts[i]
		}
	}
	return ""
}

func (v *S3SnapshotVerifier) getManifest(ctx context.Context, key string) (SnapshotManifest, error) {
	resp, err := v.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(v.storage.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return SnapshotManifest{}, err
	}
	defer resp.Body.Close()

	var manifest SnapshotManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return SnapshotManifest{}, err
	}
	return manifest, nil
}

func parseManifestTime(value string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, nil
	}
	return time.Parse("2006-01-02T15:04:05.999999-07:00", value)
}

func safeTableKey(table string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]`)
	return re.ReplaceAllString(table, "_")
}
