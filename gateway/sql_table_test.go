package main

import "testing"

func TestCriticalRecoveryTable(t *testing.T) {
	cases := map[string]string{
		"DROP TABLE users":                                "users",
		"DROP TABLE IF EXISTS public.users CASCADE":       "users",
		`DROP TABLE IF EXISTS "tenant-1"."User Accounts"`: "User Accounts",
		"TRUNCATE TABLE users":                            "users",
		"TRUNCATE public.users":                           "users",
		"DELETE FROM users":                               "users",
		"UPDATE ONLY public.users SET name = 'x'":         "users",
		"SELECT 1; DROP TABLE users":                      "users",
	}
	for query, want := range cases {
		got, err := criticalRecoveryTable(query)
		if err != nil {
			t.Fatalf("criticalRecoveryTable(%q) returned error: %v", query, err)
		}
		if got != want {
			t.Fatalf("criticalRecoveryTable(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestCriticalRecoveryTableRejectsNonTableRecovery(t *testing.T) {
	for _, query := range []string{
		"DROP DATABASE prod",
		"DROP SCHEMA public",
		"DROP TABLE users, accounts",
		"TRUNCATE users, accounts",
		"this is not sql !@#$",
		"VACUUM users",
	} {
		if _, err := criticalRecoveryTable(query); err == nil {
			t.Fatalf("criticalRecoveryTable(%q) returned nil error", query)
		}
	}
}
