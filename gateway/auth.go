package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type authContextKey struct{}

type AuthPrincipal struct {
	Name   string   `json:"name"`
	Token  string   `json:"token_id"`
	Scopes []string `json:"scopes"`
}

type tokenFile struct {
	Tokens []tokenEntry `json:"tokens"`
}

type tokenEntry struct {
	Name   string   `json:"name"`
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
}

type TokenAuthenticator struct {
	disabled bool
	tokens   []tokenEntry
}

func NewTokenAuthenticator(legacyToken, tokenFilePath string, disabled bool) (*TokenAuthenticator, error) {
	auth := &TokenAuthenticator{disabled: disabled}
	if disabled {
		return auth, nil
	}
	if strings.TrimSpace(legacyToken) != "" {
		auth.tokens = append(auth.tokens, tokenEntry{
			Name:   "legacy-admin",
			Token:  strings.TrimSpace(legacyToken),
			Scopes: []string{"admin:*"},
		})
	}
	if strings.TrimSpace(tokenFilePath) != "" {
		raw, err := os.ReadFile(tokenFilePath)
		if err != nil {
			return nil, fmt.Errorf("read auth tokens file: %w", err)
		}
		var parsed tokenFile
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("parse auth tokens file: %w", err)
		}
		for i, token := range parsed.Tokens {
			token.Name = strings.TrimSpace(token.Name)
			token.Token = strings.TrimSpace(token.Token)
			if token.Name == "" {
				return nil, fmt.Errorf("auth token %d missing name", i)
			}
			if token.Token == "" {
				return nil, fmt.Errorf("auth token %q missing token", token.Name)
			}
			if len(token.Scopes) == 0 {
				return nil, fmt.Errorf("auth token %q missing scopes", token.Name)
			}
			auth.tokens = append(auth.tokens, token)
		}
	}
	return auth, nil
}

func (a *TokenAuthenticator) Enabled() bool {
	return a != nil && !a.disabled
}

func (a *TokenAuthenticator) HasTokens() bool {
	return a != nil && (a.disabled || len(a.tokens) > 0)
}

func (a *TokenAuthenticator) Authenticate(r *http.Request) (AuthPrincipal, bool) {
	if a == nil || a.disabled {
		return AuthPrincipal{Name: "insecure-local", Token: "insecure-local", Scopes: []string{"admin:*"}}, true
	}
	candidate := bearerOrHeaderToken(r)
	if candidate == "" {
		return AuthPrincipal{}, false
	}
	for _, token := range a.tokens {
		if constantTimeEqual(candidate, token.Token) {
			return AuthPrincipal{Name: token.Name, Token: token.Name, Scopes: append([]string{}, token.Scopes...)}, true
		}
	}
	return AuthPrincipal{}, false
}

func bearerOrHeaderToken(r *http.Request) string {
	if candidate := strings.TrimSpace(r.Header.Get("X-Backstop-Token")); candidate != "" {
		return candidate
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}

func principalFromContext(ctx context.Context) (AuthPrincipal, bool) {
	principal, ok := ctx.Value(authContextKey{}).(AuthPrincipal)
	return principal, ok
}

func withPrincipal(ctx context.Context, principal AuthPrincipal) context.Context {
	return context.WithValue(ctx, authContextKey{}, principal)
}

func hasScope(principal AuthPrincipal, required string) bool {
	if required == "" {
		return true
	}
	for _, scope := range principal.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == "admin:*" || scope == required {
			return true
		}
		if strings.HasSuffix(scope, ":*") {
			prefix := strings.TrimSuffix(scope, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}

func requireScopedAuth(auth *TokenAuthenticator, metadata *MetadataStore, metrics *GatewayMetrics, requiredScope string, next http.HandlerFunc) http.HandlerFunc {
	if auth == nil || auth.disabled {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.Authenticate(r)
		if !ok {
			recordAuthDenied(r.Context(), metadata, metrics, "unauthorized", requiredScope, "")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !hasScope(principal, requiredScope) {
			recordAuthDenied(r.Context(), metadata, metrics, "forbidden", requiredScope, principal.Name)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_scope": requiredScope})
			return
		}
		next(w, r.WithContext(withPrincipal(r.Context(), principal)))
	}
}

func recordAuthDenied(ctx context.Context, metadata *MetadataStore, metrics *GatewayMetrics, status, scope, principal string) {
	if metrics != nil {
		metrics.IncBlock("auth_" + status)
	}
	if metadata != nil {
		metadata.RecordAlert(ctx, "warning", "auth_"+status, "", "recorded", map[string]any{
			"required_scope": scope,
			"principal":      principal,
		})
	}
}

func legacyValidAuthToken(r *http.Request, token string) bool {
	return constantTimeEqual(bearerOrHeaderToken(r), token)
}

func constantTimeEqual(candidate, token string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}

