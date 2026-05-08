package main

import (
	"context"
	"encoding/json"
	"fmt"
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
}

type SnapshotVerifier interface {
	VerifyLatestSidecarSnapshot(ctx context.Context, table string, snapshotID string) (SnapshotManifest, error)
}

type S3SnapshotVerifier struct {
	client  *s3.Client
	storage StorageConfig
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
	if latest.TableName != table {
		return SnapshotManifest{}, fmt.Errorf("snapshot table mismatch: manifest has %s, query targets %s", latest.TableName, table)
	}
	if latest.Writer != sidecarWriter {
		return SnapshotManifest{}, fmt.Errorf("snapshot %s was written by %s, want %s", latest.SnapshotID, latest.Writer, sidecarWriter)
	}
	if latest.SnapshotScope != "table" {
		return SnapshotManifest{}, fmt.Errorf("snapshot %s has scope %s, want table", latest.SnapshotID, latest.SnapshotScope)
	}
	if err := v.verifyDataObject(ctx, latest); err != nil {
		return SnapshotManifest{}, err
	}
	return latest, nil
}

func (v *S3SnapshotVerifier) verifyDataObject(ctx context.Context, manifest SnapshotManifest) error {
	key := strings.TrimSpace(manifest.S3DataKey)
	if key == "" && strings.HasSuffix(manifest.S3ManifestKey, "/manifest.json") {
		key = strings.TrimSuffix(manifest.S3ManifestKey, "/manifest.json") + "/data.parquet"
	}
	if key == "" {
		return fmt.Errorf("snapshot %s does not identify a data object", manifest.SnapshotID)
	}
	_, err := v.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(v.storage.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("snapshot data object verification failed for %s: %w", key, err)
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
				continue
			}
			if manifest.Writer != sidecarWriter || manifest.TableName != table {
				continue
			}
			ts, err := parseManifestTime(manifest.Timestamp)
			if err != nil {
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
