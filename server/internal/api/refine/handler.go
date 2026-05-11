// Package refine implements the /api/refine routes for the refinement chat feature.
package refine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/refine"
)

// Handler handles /api/refine routes.
type Handler struct {
	turns repo.RefinementTurnRepo
	tasks repo.TaskRepo
}

// NewHandler creates a Handler backed by the given repos.
func NewHandler(turns repo.RefinementTurnRepo, tasks repo.TaskRepo) *Handler {
	return &Handler{turns: turns, tasks: tasks}
}

// Mount registers the refinement routes on r.
// All routes require JWT auth — mount inside a protected group.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/refine/{taskId}/turns", h.listTurns)
	r.Post("/api/refine/{taskId}/turn", h.submitTurn)
	r.Post("/api/refine/{taskId}/confirm", h.confirm)
}

// TurnResponse is the JSON shape returned by GET /api/refine/{taskId}/turns.
type TurnResponse struct {
	ID        string  `json:"id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Phase     *string `json:"phase,omitempty"`
	CreatedAt string  `json:"created_at"` // RFC3339
}

// GET /api/refine/{taskId}/turns
func (h *Handler) listTurns(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.PayloadFromContext(r.Context())
	if !ok {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	taskID := chi.URLParam(r, "taskId")
	turns, err := h.turns.ListForTask(r.Context(), taskID, 0)
	if err != nil {
		jsonError(w, "failed to list turns", http.StatusInternalServerError)
		return
	}

	resp := make([]TurnResponse, 0, len(turns))
	for _, t := range turns {
		resp = append(resp, TurnResponse{
			ID:        t.ID,
			Role:      string(t.Role),
			Content:   t.Content,
			Phase:     t.Phase,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// POST /api/refine/{taskId}/turn — submit a user message and stream the assistant response via SSE.
func (h *Handler) submitTurn(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.PayloadFromContext(r.Context())
	if !ok {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	taskID := chi.URLParam(r, "taskId")

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Message) == "" {
		jsonError(w, "message is required", http.StatusBadRequest)
		return
	}

	// Fetch task for title + description + working directory.
	task, err := h.tasks.GetByID(r.Context(), taskID)
	if err != nil {
		jsonError(w, "task not found", http.StatusNotFound)
		return
	}

	// Store the user turn.
	userRole := "user"
	if _, err := h.turns.Create(r.Context(), repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    userRole,
		Content: body.Message,
	}); err != nil {
		jsonError(w, "failed to store user turn", http.StatusInternalServerError)
		return
	}

	// Fetch windowed history (last 20 turns) for context.
	history, err := h.turns.ListForTask(r.Context(), taskID, 20)
	if err != nil {
		jsonError(w, "failed to fetch history", http.StatusInternalServerError)
		return
	}

	turns := make([]refine.Turn, 0, len(history))
	for _, t := range history {
		turns = append(turns, refine.Turn{
			Role:    string(t.Role),
			Content: t.Content,
		})
	}

	workDir := task.Cwd
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	var desc string
	if task.Description != nil {
		desc = *task.Description
	}

	cfg := refine.SpawnConfig{
		TaskTitle:       task.Title,
		TaskDescription: desc,
		History:         turns,
		UserMessage:     body.Message,
		WorkDir:         workDir,
	}

	// Set SSE headers before starting the stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	stream, err := refine.RunRefinementTurn(r.Context(), cfg)
	if err != nil {
		// Headers already set for SSE — send error as SSE event.
		fmt.Fprintf(w, "data: [ERROR] %s\n\n", err.Error())
		if canFlush {
			flusher.Flush()
		}
		return
	}

	var sb strings.Builder
	for {
		select {
		case line, open := <-stream:
			if !open {
				goto done
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			fmt.Fprintf(w, "data: %s\n\n", line)
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}

done:
	// Store the full assistant response as a turn.
	assistantRole := "assistant"
	fullResponse := strings.TrimRight(sb.String(), "\n")
	if fullResponse != "" {
		_, _ = h.turns.Create(r.Context(), repo.CreateTurnInput{
			TaskID:  taskID,
			Role:    assistantRole,
			Content: fullResponse,
		})
	}
}

// POST /api/refine/{taskId}/confirm — mark refinement as confirmed.
func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.PayloadFromContext(r.Context())
	if !ok {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	taskID := chi.URLParam(r, "taskId")
	phase := "confirmed"
	if _, err := h.turns.Create(r.Context(), repo.CreateTurnInput{
		TaskID:  taskID,
		Role:    "assistant",
		Content: "confirmed",
		Phase:   &phase,
	}); err != nil {
		jsonError(w, "failed to store confirmation turn", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
