package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeTranscriber struct{ text string }

func (f fakeTranscriber) Transcribe(_ context.Context, _ string) (string, error) {
	return f.text, nil
}

func TestHealth(t *testing.T) {
	srv := NewServer(fakeTranscriber{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
}

func TestTranscribeReturnsText(t *testing.T) {
	srv := NewServer(fakeTranscriber{text: "hello world"})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("audio", "clip.webm")
	fw.Write([]byte("fake-audio-bytes"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/transcribe", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("transcribe = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Text != "hello world" {
		t.Fatalf("text = %q, want %q", out.Text, "hello world")
	}
}

func TestAddonJsServed(t *testing.T) {
	srv := NewServer(fakeTranscriber{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/addon.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("addon.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript" {
		t.Fatalf("content-type = %q, want text/javascript", ct)
	}
}
