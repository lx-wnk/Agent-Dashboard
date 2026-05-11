// Package channel implements the dashboard-channel MCP server (stdio).
//
// The bridge is invoked as a subprocess by Claude Code via a temporary MCP config file
// written by the pipeline spawner. It runs alongside the spawned stage agent and provides:
//
//   - dashboard_reply: let the agent send status updates back to the dashboard.
//   - request_permission: forward bulk permission requests to the dashboard server.
//   - HTTP server: receive dashboard → agent messages (forwarded as MCP log notifications).
//   - Discovery file: ~/.claude/dashboard-channel/{parentPid}.json so the dashboard can
//     find the HTTP server's port and ephemeral token.
//
// Call Run to start the bridge; it blocks until the MCP session ends or the context is
// cancelled.
package channel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const discoveryDir = ".claude/dashboard-channel"

// Run starts the channel bridge. It blocks until the MCP stdio session ends.
func Run(ctx context.Context) error {
	dashboardURL := strings.TrimSuffix(os.Getenv("DASHBOARD_MCP_URL"), "/")
	if dashboardURL == "" {
		dashboardURL = "http://127.0.0.1:13120"
	}
	mcpToken := os.Getenv("DASHBOARD_MCP_TOKEN")
	stageRunID := os.Getenv("DASHBOARD_STAGE_RUN_ID")
	httpToken := generateToken()
	parentPid := os.Getppid()

	var sessionPtr atomic.Pointer[mcp.ServerSession]

	httpSrv, httpPort, err := startHTTPServer(dashboardURL, httpToken, &sessionPtr)
	if err != nil {
		return fmt.Errorf("channel: HTTP server: %w", err)
	}

	discPath, err := writeDiscovery(parentPid, httpPort, httpToken)
	if err != nil {
		slog.Warn("channel: discovery file write failed", "err", err)
	}

	cleanup := func() {
		if discPath != "" {
			_ = os.Remove(discPath)
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}
	defer cleanup()

	// Honour OS signals in addition to the context.
	go func() {
		sigC := make(chan os.Signal, 1)
		signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)
		select {
		case <-sigC:
		case <-ctx.Done():
		}
		cleanup()
		os.Exit(0)
	}()

	server := mcp.NewServer(&mcp.Implementation{Name: "dashboard-channel", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: channelInstructions,
	})

	// Capture session on every request so the HTTP handler can send notifications.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if ss, ok := req.GetSession().(*mcp.ServerSession); ok {
				sessionPtr.Store(ss)
			}
			return next(ctx, method, req)
		}
	})

	// dashboard_reply tool
	type replyArgs struct {
		Message string `json:"message" jsonschema:"description=Reply message to display in the dashboard,required"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dashboard_reply",
		Description: "Send a reply back to the monitoring dashboard. Use when you complete a dashboard instruction or want to report progress.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args replyArgs) (*mcp.CallToolResult, any, error) {
		if args.Message == "" {
			return errResult("message is required"), nil, nil
		}
		if err := postReply(dashboardURL, mcpToken, parentPid, args.Message); err != nil {
			return textResult("dashboard unreachable: " + err.Error()), nil, nil
		}
		return textResult("Reply sent to dashboard."), nil, nil
	})

	// request_permission tool
	type permEntry struct {
		Tool    string  `json:"tool"`
		Pattern *string `json:"pattern,omitempty"`
		Reason  *string `json:"reason,omitempty"`
	}
	type reqPermArgs struct {
		StageRunID  *string     `json:"stageRunId,omitempty"`
		Permissions []permEntry `json:"permissions,omitempty"`
		Tool        *string     `json:"tool,omitempty"`
		Pattern     *string     `json:"pattern,omitempty"`
		Reason      *string     `json:"reason,omitempty"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_permission",
		Description: requestPermDesc,
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reqPermArgs) (*mcp.CallToolResult, any, error) {
		sid := stageRunID
		if args.StageRunID != nil && *args.StageRunID != "" {
			sid = *args.StageRunID
		}
		if sid == "" {
			return errResult("No stageRunId — task is not orchestrator-managed."), nil, nil
		}

		var entries []permEntry
		switch {
		case len(args.Permissions) > 0:
			entries = args.Permissions
		case args.Tool != nil && *args.Tool != "":
			entries = []permEntry{{Tool: *args.Tool, Pattern: args.Pattern, Reason: args.Reason}}
		default:
			return errResult("request_permission needs `permissions: [...]` or single-tool `tool`+`reason`."), nil, nil
		}

		resp, apiErr := callDashboard(dashboardURL, mcpToken, "POST", "/api/permission-requests/bulk",
			map[string]any{"stageRunId": sid, "entries": entries})
		if apiErr != nil {
			return textResult("Could not reach dashboard: " + apiErr.Error()), nil, nil
		}
		return textResult(resp), nil, nil
	})

	slog.Info("channel: MCP stdio server starting", "parentPid", parentPid, "httpPort", httpPort)
	return server.Run(ctx, &mcp.StdioTransport{})
}

// ─── HTTP server ──────────────────────────────────────────────────────────────

func startHTTPServer(
	dashboardURL, httpToken string,
	sess *atomic.Pointer[mcp.ServerSession],
) (*http.Server, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})

	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		var payload struct{ Message string `json:"message"` }
		if err := json.Unmarshal(body, &payload); err != nil || payload.Message == "" {
			http.Error(w, `{"error":"missing message"}`, http.StatusBadRequest)
			return
		}

		// Forward as MCP log notification (best-effort).
		if ss := sess.Load(); ss != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			_ = ss.Log(ctx, &mcp.LoggingMessageParams{
				Level:  "info",
				Logger: "dashboard",
				Data:   json.RawMessage(`"` + escapeJSON(payload.Message) + `"`),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	mux.HandleFunc("OPTIONS /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", corsOrigin(r, dashboardURL))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
	})

	srv := &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("channel: HTTP server error", "err", err)
		}
	}()
	return srv, port, nil
}

func corsOrigin(r *http.Request, dashboardURL string) string {
	o := r.Header.Get("Origin")
	if strings.HasPrefix(o, "http://127.0.0.1") || strings.HasPrefix(o, "http://localhost") {
		return o
	}
	return dashboardURL
}

// ─── Discovery ────────────────────────────────────────────────────────────────

func writeDiscovery(parentPid, port int, token string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("UserHomeDir: %w", err)
	}
	dir := filepath.Join(home, discoveryDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	path := filepath.Join(dir, strconv.Itoa(parentPid)+".json")
	data, _ := json.Marshal(map[string]any{
		"port":       port,
		"channelPid": os.Getpid(),
		"parentPid":  parentPid,
		"cwd":        cwd(),
		"token":      token,
		"startedAt":  time.Now().UTC().Format(time.RFC3339),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return path, nil
}

func cwd() string { d, _ := os.Getwd(); return d }

// ─── Dashboard HTTP helpers ───────────────────────────────────────────────────

func callDashboard(baseURL, token, method, path string, body any) (string, error) {
	var br io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("marshal: %w", err)
		}
		br = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, baseURL+path, br)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return formatPermResponse(respBody), nil
}

func formatPermResponse(body []byte) string {
	var v struct {
		AutoResolved []struct {
			Tool    string  `json:"tool"`
			Pattern *string `json:"pattern"`
		} `json:"autoResolved"`
		Pending []struct {
			Tool    string  `json:"tool"`
			Pattern *string `json:"pattern"`
		} `json:"pending"`
		LoopWarning *string `json:"loopWarning"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	fmtList := func(items []struct {
		Tool    string  `json:"tool"`
		Pattern *string `json:"pattern"`
	}) []string {
		names := make([]string, 0, len(items))
		for _, e := range items {
			s := e.Tool
			if e.Pattern != nil && *e.Pattern != "" {
				s += "(" + *e.Pattern + ")"
			}
			names = append(names, s)
		}
		return names
	}
	autoMsg := "no auto-resolves"
	if len(v.AutoResolved) > 0 {
		autoMsg = fmt.Sprintf("%d auto-resolved: %s", len(v.AutoResolved), strings.Join(fmtList(v.AutoResolved), ", "))
	}
	pendMsg := "all granted — proceed"
	if len(v.Pending) > 0 {
		pendMsg = fmt.Sprintf("%d ON HOLD awaiting user: %s", len(v.Pending), strings.Join(fmtList(v.Pending), ", "))
	}
	loopMsg := ""
	if v.LoopWarning != nil && *v.LoopWarning != "" {
		loopMsg = "\n\n⚠️ " + *v.LoopWarning
	}
	return autoMsg + ". " + pendMsg + "." + loopMsg
}

func postReply(dashboardURL, token string, parentPid int, message string) error {
	_, err := callDashboard(dashboardURL, token, "POST", "/api/channel-reply", map[string]any{
		"parentPid": parentPid,
		"message":   message,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	return err
}

// ─── Misc ─────────────────────────────────────────────────────────────────────

func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("channel: generateToken: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1]) // strip surrounding quotes
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

const channelInstructions = `Messages from the monitoring dashboard arrive as MCP log notifications (level: info, logger: dashboard).

## How to handle dashboard messages

1. FINISH your current action before addressing the message.
2. ACKNOWLEDGE immediately with dashboard_reply (under 100 chars).
3. ASSESS priority: course correction, hint, question, or directive.
4. REPORT progress after each significant step.
5. REPORT completion with a final dashboard_reply.

## Reply format
Use dashboard_reply for all communication. Keep replies under 200 chars, plain text, no markdown.`

const requestPermDesc = `Request one or more tool permissions. CRITICAL: every missed permission triggers a kill-and-restart — request piecemeal and you burn through restart loops.

Mandatory pattern:
  1. As your FIRST action, enumerate every tool you will need.
  2. Call ONCE with the full permissions: [...] array. Pre-granted entries auto-resolve.
  3. On [PERMISSION GRANTED] resume: re-scan remaining work and bulk-request everything else.

Single-tool form (tool + pattern) is kept for compatibility but strongly discouraged.`
