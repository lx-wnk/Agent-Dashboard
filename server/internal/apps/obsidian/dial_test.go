package obsidian

import (
	"context"
	"net"
	"testing"
)

// TestDialPolicyRefusesAnyOtherHost proves the dial function never re-resolves
// the configured host: it string-matches the requested address against the
// host and IP pinned at construction and otherwise refuses, so a DNS answer
// that changed for that host between resolve and connect cannot matter — it
// is never consulted again.
func TestDialPolicyRefusesAnyOtherHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	pinnedIP := net.ParseIP("127.0.0.1")
	dial := dialPolicy("configured.example", pinnedIP, port)

	conn, err := dial(context.Background(), "tcp", net.JoinHostPort("configured.example", port))
	if err != nil {
		t.Fatalf("dial pinned host: unexpected error: %v", err)
	}
	_ = conn.Close()

	if _, err := dial(context.Background(), "tcp", net.JoinHostPort("attacker.example", port)); err == nil {
		t.Fatal("dial: want refusal for a host other than the configured one")
	}
	if _, err := dial(context.Background(), "tcp", net.JoinHostPort("10.0.0.9", port)); err == nil {
		t.Fatal("dial: want refusal for an IP other than the pinned one")
	}
	if _, err := dial(context.Background(), "tcp", net.JoinHostPort("configured.example", "9999")); err == nil {
		t.Fatal("dial: want refusal for a port other than the configured one")
	}
}

func TestResolveVaultPathRefusesEscape(t *testing.T) {
	if _, err := resolveVaultPath("notes", "../secrets/passwords.md"); err == nil {
		t.Fatal("resolveVaultPath: want refusal for a path escaping the vault root")
	}
	if _, err := resolveVaultPath("notes", "../../etc/passwd"); err == nil {
		t.Fatal("resolveVaultPath: want refusal for a path escaping the vault root")
	}
	if _, err := resolveVaultPath("notes", ""); err == nil {
		t.Fatal("resolveVaultPath: want refusal for an empty note path")
	}
	if _, err := resolveVaultPath("", "a.md"); err == nil {
		t.Fatal("resolveVaultPath: want refusal for an empty vault root")
	}

	got, err := resolveVaultPath("notes", "a/b.md")
	if err != nil {
		t.Fatalf("resolveVaultPath: unexpected error: %v", err)
	}
	if got != "notes/a/b.md" {
		t.Fatalf("resolveVaultPath: got %q, want %q", got, "notes/a/b.md")
	}
}
