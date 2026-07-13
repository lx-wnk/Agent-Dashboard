package oauthkit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testCookie = "test_oauth_state"

func TestRandomState_UniqueNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		s, err := RandomState()
		if err != nil {
			t.Fatalf("RandomState: %v", err)
		}
		if s == "" {
			t.Fatal("empty state")
		}
		if seen[s] {
			t.Fatalf("duplicate state %q", s)
		}
		seen[s] = true
	}
}

func TestStartLogin_MissingNonce(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	if _, ok := StartLogin(rr, req, testCookie); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	assertBody(t, rr, "missing nonce")
}

func TestStartLogin_SetsCookieCarryingNonce(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login?nonce=jwt.with.dots", nil)
	stateValue, ok := StartLogin(rr, req, testCookie)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.HasSuffix(stateValue, ".jwt.with.dots") {
		t.Fatalf("state value should carry nonce, got %q", stateValue)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != testCookie || cookies[0].Value != stateValue {
		t.Fatalf("expected state cookie %q=%q, got %+v", testCookie, stateValue, cookies)
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != StateCookieMaxAge {
		t.Fatalf("cookie flags wrong: %+v", cookies[0])
	}
}

func TestValidateState_MissingCookie(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?state=x", nil)
	if _, ok := ValidateState(rr, req, testCookie); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	assertBody(t, rr, "missing state cookie")
}

func TestValidateState_Mismatch(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?state=attacker", nil)
	req.AddCookie(&http.Cookie{Name: testCookie, Value: "csrf.nonce"})
	if _, ok := ValidateState(rr, req, testCookie); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	assertBody(t, rr, "state mismatch")
}

func TestValidateState_NoDot(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?state=nodot", nil)
	req.AddCookie(&http.Cookie{Name: testCookie, Value: "nodot"})
	if _, ok := ValidateState(rr, req, testCookie); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	assertBody(t, rr, "malformed state: missing nonce")
}

func TestValidateState_EmptyNonce(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?state=csrf.", nil)
	req.AddCookie(&http.Cookie{Name: testCookie, Value: "csrf."})
	if _, ok := ValidateState(rr, req, testCookie); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	assertBody(t, rr, "malformed state: empty nonce")
}

func TestValidateState_SuccessRecoversNonceAndClearsCookie(t *testing.T) {
	rr := httptest.NewRecorder()
	const state = "csrf.jwt.with.dots"
	req := httptest.NewRequest(http.MethodGet, "/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: testCookie, Value: state})
	nonce, ok := ValidateState(rr, req, testCookie)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if nonce != "jwt.with.dots" {
		t.Fatalf("expected nonce with dots preserved, got %q", nonce)
	}
	cleared := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == testCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("state cookie not cleared")
	}
}

func TestExtractCode_ProviderError(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?error=access_denied", nil)
	if _, ok := ExtractCode(rr, req); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	assertBody(t, rr, "authentication denied by provider")
}

func TestExtractCode_MissingCode(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	if _, ok := ExtractCode(rr, req); ok {
		t.Fatal("expected ok=false")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	assertBody(t, rr, "missing code")
}

func TestExtractCode_Success(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc123", nil)
	code, ok := ExtractCode(rr, req)
	if !ok || code != "abc123" {
		t.Fatalf("expected code=abc123 ok=true, got %q %v", code, ok)
	}
}

func TestCreateSession_Success(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session-jwt"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req := SessionRequest{ProviderID: "42", Login: "octocat", DisplayName: "Octo Cat", AvatarURL: "http://a/x.png"}
	cookie, err := CreateSession(context.Background(), srv.Client(), srv.URL, "shared-secret-value-at-least-32chars!", req, "the-nonce")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if cookie.Name != "auth_token" || cookie.Value != "session-jwt" {
		t.Fatalf("wrong cookie: %+v", cookie)
	}
	if gotAuth != "Bearer shared-secret-value-at-least-32chars!" {
		t.Fatalf("wrong Authorization header: %q", gotAuth)
	}
	for _, want := range []string{`"provider_id":"42"`, `"login":"octocat"`, `"nonce":"the-nonce"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body missing %s, got %s", want, gotBody)
		}
	}
}

func TestCreateSession_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := CreateSession(context.Background(), srv.Client(), srv.URL, "s", SessionRequest{}, "n")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected HTTP 403 error, got %v", err)
	}
}

func TestCreateSession_MissingCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, err := CreateSession(context.Background(), srv.Client(), srv.URL, "s", SessionRequest{}, "n")
	if err == nil || !strings.Contains(err.Error(), "auth_token cookie missing") {
		t.Fatalf("expected missing-cookie error, got %v", err)
	}
}

func assertBody(t *testing.T, rr *httptest.ResponseRecorder, contains string) {
	t.Helper()
	if !strings.Contains(rr.Body.String(), contains) {
		t.Errorf("expected body to contain %q, got: %s", contains, rr.Body.String())
	}
}
