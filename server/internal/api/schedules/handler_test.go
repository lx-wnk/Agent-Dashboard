package schedules_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/schedules"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/scheduler"
)

func newServerWithApps(t *testing.T) (*httptest.Server, repo.MCPApplicationRepo) {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	apps := repo.NewMCPApplicationRepo(bundle.Client)
	h := schedules.NewHandler(
		repo.NewTaskScheduleRepo(bundle.Client),
		scheduler.NewNLCron(nil),
		nil,
		apps,
		true,
	)
	r := chi.NewRouter()
	h.Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, apps
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newServerWithApps(t)
	return srv
}

func scheduleWith(applications []string) map[string]any {
	return map[string]any{
		"name": "inbox", "cronExpr": "*/5 * * * *", "slugPrefix": "inbox",
		"title": "Inbox", "cwd": "/tmp", "applications": applications,
	}
}

func post(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestPreview_ValidPhrase(t *testing.T) {
	srv := newServer(t)
	resp := post(t, srv.URL+"/api/schedules/preview", map[string]any{"nlText": "every weekday at 9am"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		CronExpr string   `json:"cronExpr"`
		NextRuns []string `json:"nextRuns"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.CronExpr != "0 9 * * 1-5" {
		t.Fatalf("cronExpr = %q", out.CronExpr)
	}
	if len(out.NextRuns) != 5 {
		t.Fatalf("want 5 next runs, got %d", len(out.NextRuns))
	}
}

func TestPreview_InvalidPhrase422(t *testing.T) {
	srv := newServer(t)
	resp := post(t, srv.URL+"/api/schedules/preview", map[string]any{"nlText": "whenever the mood strikes"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestCreateAndList(t *testing.T) {
	srv := newServer(t)
	resp := post(t, srv.URL+"/api/schedules", map[string]any{
		"name":       "nightly build",
		"nlText":     "every day at 2am",
		"slugPrefix": "nightly",
		"title":      "Nightly build",
		"cwd":        "/tmp",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID        string `json:"id"`
		CronExpr  string `json:"cronExpr"`
		NextRunAt string `json:"nextRunAt"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.CronExpr != "0 2 * * *" {
		t.Fatalf("cronExpr = %q", created.CronExpr)
	}
	if created.NextRunAt == "" {
		t.Fatal("expected next_run_at to be initialized on create")
	}

	listResp, err := http.Get(srv.URL + "/api/schedules")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer listResp.Body.Close()
	var list []map[string]any
	_ = json.NewDecoder(listResp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("want 1 schedule, got %d", len(list))
	}
}

func TestCreate_AcceptsKnownApplications(t *testing.T) {
	srv, apps := newServerWithApps(t)
	_, err := apps.Upsert(context.Background(), repo.UpsertMCPApplicationInput{ResourceID: "res-mail", ServerName: "mail"})
	if err != nil {
		t.Fatal(err)
	}

	resp := post(t, srv.URL+"/api/schedules", scheduleWith([]string{"res-mail"}))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Applications []string `json:"applications"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Applications) != 1 || got.Applications[0] != "res-mail" {
		t.Fatalf("applications = %v", got.Applications)
	}
}

func TestCreate_RejectsUnknownApplication(t *testing.T) {
	srv, _ := newServerWithApps(t)
	resp := post(t, srv.URL+"/api/schedules", scheduleWith([]string{"res-does-not-exist"}))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — an id that names no application must not be stored", resp.StatusCode)
	}
}

func TestCreate_InvalidPhrase422(t *testing.T) {
	srv := newServer(t)
	resp := post(t, srv.URL+"/api/schedules", map[string]any{
		"name":       "bad",
		"nlText":     "sometime maybe",
		"slugPrefix": "bad",
		"title":      "Bad",
		"cwd":        "/tmp",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}
