package serverapp

import (
	"encoding/json"
	"testing"
)

func TestPushPermissionPayload(t *testing.T) {
	b := pushPermissionPayload("t1", "Triage inbox", "mcp__mail__imap_move_email")

	var got map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["title"] != "Approval needed" {
		t.Errorf("title = %q, want %q", got["title"], "Approval needed")
	}
	if got["body"] != "Triage inbox: mcp__mail__imap_move_email" {
		t.Errorf("body = %q, want %q", got["body"], "Triage inbox: mcp__mail__imap_move_email")
	}
	if got["url"] != "/" {
		t.Errorf("url = %q, want %q", got["url"], "/")
	}
	if got["tag"] != "permission-t1" {
		t.Errorf("tag = %q, want %q", got["tag"], "permission-t1")
	}
}

func TestNewPushPermissionNotifier_NilServiceReturnsNilInterface(t *testing.T) {
	if n := newPushPermissionNotifier(nil); n != nil {
		t.Errorf("expected nil interface, got %#v", n)
	}
}
