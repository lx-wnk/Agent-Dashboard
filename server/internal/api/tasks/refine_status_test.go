package tasks

import (
	"testing"
)

type stubRefineReader struct{ status, errMsg string }

func (s stubRefineReader) State(string) (string, string) { return s.status, s.errMsg }

func TestApplyRefineStatus_SetsFields(t *testing.T) {
	h := &Handler{refineReader: stubRefineReader{status: "running"}}
	e := &EnrichedTask{}
	h.applyRefineStatus(e, "t")
	if e.RefineStatus == nil || *e.RefineStatus != "running" {
		t.Fatalf("expected RefineStatus=running, got %v", e.RefineStatus)
	}
	if e.RefineError != nil {
		t.Fatalf("expected RefineError=nil, got %v", e.RefineError)
	}
}

func TestApplyRefineStatus_SetsErrorField(t *testing.T) {
	h := &Handler{refineReader: stubRefineReader{status: "failed", errMsg: "context deadline exceeded"}}
	e := &EnrichedTask{}
	h.applyRefineStatus(e, "t")
	if e.RefineStatus == nil || *e.RefineStatus != "failed" {
		t.Fatalf("expected RefineStatus=failed, got %v", e.RefineStatus)
	}
	if e.RefineError == nil || *e.RefineError != "context deadline exceeded" {
		t.Fatalf("expected RefineError=context deadline exceeded, got %v", e.RefineError)
	}
}

func TestApplyRefineStatus_NilReaderNoPanic(t *testing.T) {
	h := &Handler{}
	h.applyRefineStatus(&EnrichedTask{}, "t")
	// must not panic
}

func TestApplyRefineStatus_NilTaskNoPanic(t *testing.T) {
	h := &Handler{refineReader: stubRefineReader{status: "idle"}}
	h.applyRefineStatus(nil, "t")
	// must not panic
}
