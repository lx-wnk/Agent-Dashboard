package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
}

func TestAddonJsServed(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/addon.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("addon.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript" {
		t.Fatalf("content-type = %q, want text/javascript", ct)
	}
}
