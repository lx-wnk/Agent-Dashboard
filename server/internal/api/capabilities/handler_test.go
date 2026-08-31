package capabilities

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/askgate"
	"github.com/lx-wnk/agent-dashboard/server/internal/serverask"
)

// fakeResolver records what it was handed and returns a fixed error.
type fakeResolver struct {
	called   bool
	gotID    string
	gotDecs  string
	returnEr error
}

func (f *fakeResolver) Resolve(id, decision string) error {
	f.called = true
	f.gotID = id
	f.gotDecs = decision
	return f.returnEr
}

func doRequest(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/decisions/respond", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Respond(rec, req)
	return rec
}

func TestRespond_AllowAndDenyReachResolverVerbatim(t *testing.T) {
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			resolver := &fakeResolver{}
			h := New(resolver)
			rec := doRequest(t, h, `{"id":"abc-123","decision":"`+decision+`"}`)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if !resolver.called {
				t.Fatal("resolver was not called")
			}
			if resolver.gotID != "abc-123" || resolver.gotDecs != decision {
				t.Fatalf("resolver got (%q, %q), want (%q, %q)", resolver.gotID, resolver.gotDecs, "abc-123", decision)
			}
		})
	}
}

func TestRespond_UnknownDecisionRejectedWithoutCallingResolver(t *testing.T) {
	resolver := &fakeResolver{}
	h := New(resolver)
	rec := doRequest(t, h, `{"id":"abc-123","decision":"maybe"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if resolver.called {
		t.Fatal("resolver was called for an unknown decision — validation must happen at the boundary")
	}
}

func TestRespond_ErrNotPendingMapsTo404(t *testing.T) {
	resolver := &fakeResolver{returnEr: askgate.ErrNotPending}
	h := New(resolver)
	rec := doRequest(t, h, `{"id":"abc-123","decision":"allow"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRespond_ErrInvalidDecisionMapsTo400(t *testing.T) {
	resolver := &fakeResolver{returnEr: serverask.ErrInvalidDecision}
	h := New(resolver)
	rec := doRequest(t, h, `{"id":"abc-123","decision":"allow"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRespond_UnexpectedErrorMapsTo500WithoutLeakingText(t *testing.T) {
	secret := "some sensitive internal detail"
	resolver := &fakeResolver{returnEr: errors.New(secret)}
	h := New(resolver)
	rec := doRequest(t, h, `{"id":"abc-123","decision":"allow"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("response body leaked the internal error text: %s", rec.Body.String())
	}
}

func TestRespond_MalformedJSONIs400(t *testing.T) {
	resolver := &fakeResolver{}
	h := New(resolver)
	rec := doRequest(t, h, `{not json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if resolver.called {
		t.Fatal("resolver was called for malformed JSON")
	}
}
