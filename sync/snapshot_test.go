package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotManifestMatchesPhase2Contract(t *testing.T) {
	manifest := SnapshotManifest{
		ManifestVersion:  1,
		Writer:           sidecarWriter,
		DBName:           "appdb",
		SchemaName:       "public",
		SnapshotID:       "snap_1234abcd",
		Timestamp:        "2026-05-01T00:00:00Z",
		TableName:        "users",
		Query:            "SYNC SNAPSHOT public.users",
		Operation:        "SYNC_SNAPSHOT",
		Actor:            nil,
		RowCount:         2,
		SchemaDDL:        `CREATE TABLE "users" ("id" INTEGER)`,
		FKConstraints:    []string{},
		Indexes:          []string{},
		CheckConstraints: []string{},
		S3Bucket:         "backstop-test",
		S3DataKey:        "backstop/snapshots/users/snap_1234abcd/data.parquet",
		S3ManifestKey:    "backstop/snapshots/users/snap_1234abcd/manifest.json",
		DataSHA256:       strings.Repeat("a", 64),
		SnapshotScope:    "table",
		Status:           "valid",
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	required := []string{
		"manifest_version",
		"writer",
		"db_name",
		"schema_name",
		"snapshot_id",
		"timestamp",
		"table_name",
		"snapshot_scope",
		"operation",
		"query",
		"actor",
		"row_count",
		"schema_ddl",
		"fk_constraints",
		"check_constraints",
		"indexes",
		"s3_bucket",
		"s3_data_key",
		"s3_manifest_key",
		"data_sha256",
		"status",
	}
	for _, key := range required {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("manifest missing required key %q: %s", key, string(raw))
		}
	}
	if decoded["writer"] != sidecarWriter {
		t.Fatalf("writer = %v, want %s", decoded["writer"], sidecarWriter)
	}
	if decoded["operation"] != "SYNC_SNAPSHOT" {
		t.Fatalf("operation = %v, want SYNC_SNAPSHOT", decoded["operation"])
	}
}

func TestSnapshotManifestSupportsExplicitVerificationFreshness(t *testing.T) {
	manifest := SnapshotManifest{
		SnapshotID:    "snap_1234abcd",
		TableName:     "users",
		Timestamp:     "2026-05-01T00:00:00Z",
		VerifiedAt:    "2026-05-01T00:01:00Z",
		DataSHA256:    strings.Repeat("a", 64),
		Status:        "valid",
		S3DataKey:     "backstop/snapshots/users/snap_1234abcd/data.parquet",
		S3Bucket:      "backstop-test",
		SnapshotScope: "table",
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if !strings.Contains(string(raw), `"verified_at":"2026-05-01T00:01:00Z"`) {
		t.Fatalf("manifest should expose verification freshness: %s", raw)
	}
}

func TestNonNilStringsKeepsManifestArraysRestoreCompatible(t *testing.T) {
	manifest := SnapshotManifest{
		FKConstraints:    nonNilStrings(nil),
		Indexes:          nonNilStrings(nil),
		CheckConstraints: nonNilStrings(nil),
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	for _, disallowed := range []string{
		`"fk_constraints":null`,
		`"indexes":null`,
		`"check_constraints":null`,
	} {
		if strings.Contains(string(raw), disallowed) {
			t.Fatalf("manifest contains non-restore-compatible null array: %s", raw)
		}
	}
}

func TestSnapshotKeyHelpers(t *testing.T) {
	if got := safeTableKey(`bad table/"name"`); got != "bad_table__name_" {
		t.Fatalf("safeTableKey = %q, want bad_table__name_", got)
	}
	if got := quoteIdent(`bad"name`); got != `"bad""name"` {
		t.Fatalf("quoteIdent = %q", got)
	}
}

func TestParquetSchemaUsesOptionalUTF8Columns(t *testing.T) {
	schema := parquetSchema([]string{"id", "name"})
	for _, want := range []string{
		"name=parquet_go_root",
		"name=id, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL",
		"name=name, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema %q does not contain %q", schema, want)
		}
	}
}
