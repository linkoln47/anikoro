package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithSecurityHeadersSetsContentSecurityPolicy(t *testing.T) {
	handler := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)

	handler.ServeHTTP(recorder, request)

	policy := recorder.Header().Get("Content-Security-Policy")
	requiredDirectives := []string{
		"default-src 'self'",
		"script-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	}

	for _, directive := range requiredDirectives {
		if !strings.Contains(policy, directive) {
			t.Fatalf("Content-Security-Policy %q does not contain %q", policy, directive)
		}
	}
}
