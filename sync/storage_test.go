package main

import "testing"

func TestParseStorageBucketOnly(t *testing.T) {
	cfg, err := ParseStorage("s3://backstop-test", "", "backstop")
	if err != nil {
		t.Fatalf("ParseStorage returned error: %v", err)
	}
	if cfg.Bucket != "backstop-test" {
		t.Fatalf("bucket = %q, want backstop-test", cfg.Bucket)
	}
	if cfg.Endpoint != "" {
		t.Fatalf("endpoint = %q, want empty", cfg.Endpoint)
	}
	if cfg.Prefix != "backstop" {
		t.Fatalf("prefix = %q, want backstop", cfg.Prefix)
	}
}

func TestParseStorageWithEndpoint(t *testing.T) {
	cfg, err := ParseStorage("s3://backstop-test@http://localhost:9000", "", "/custom/")
	if err != nil {
		t.Fatalf("ParseStorage returned error: %v", err)
	}
	if cfg.Bucket != "backstop-test" {
		t.Fatalf("bucket = %q, want backstop-test", cfg.Bucket)
	}
	if cfg.Endpoint != "http://localhost:9000" {
		t.Fatalf("endpoint = %q, want http://localhost:9000", cfg.Endpoint)
	}
	if cfg.Prefix != "custom" {
		t.Fatalf("prefix = %q, want custom", cfg.Prefix)
	}
}

func TestParseStorageFallbackEndpoint(t *testing.T) {
	cfg, err := ParseStorage("s3://backstop-test", "http://localhost:9000", "backstop")
	if err != nil {
		t.Fatalf("ParseStorage returned error: %v", err)
	}
	if cfg.Endpoint != "http://localhost:9000" {
		t.Fatalf("endpoint = %q, want fallback endpoint", cfg.Endpoint)
	}
}

func TestParseStorageRejectsInvalidInput(t *testing.T) {
	cases := []string{"", "backstop-test", "s3://", "s3://bucket@not a url"}
	for _, tc := range cases {
		if _, err := ParseStorage(tc, "", "backstop"); err == nil {
			t.Fatalf("ParseStorage(%q) returned nil error", tc)
		}
	}
}

