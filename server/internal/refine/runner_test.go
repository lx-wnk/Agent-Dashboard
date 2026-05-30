package refine

import (
	"context"
	"testing"
)

func TestRunner_State_DefaultsToIdle(t *testing.T) {
	r := NewRunner(nil, nil)
	status, errMsg := r.State("task-x")
	if status != StatusIdle {
		t.Errorf("default status: got %q, want %q", status, StatusIdle)
	}
	if errMsg != "" {
		t.Errorf("default errMsg: got %q, want empty", errMsg)
	}
}

func TestRunner_IsRunning_FalseWhenAbsent(t *testing.T) {
	r := NewRunner(nil, nil)
	if r.IsRunning("task-x") {
		t.Error("IsRunning should be false for an unknown task")
	}
	_ = context.Background()
}
