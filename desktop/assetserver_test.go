//go:build darwin

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	wailsassets "github.com/wailsapp/wails/v2/pkg/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

type nopLogger struct{}

func (nopLogger) Debug(string, ...interface{}) {}
func (nopLogger) Error(string, ...interface{}) {}

type nopRuntime struct{}

func (nopRuntime) DesktopIPC() []byte       { return []byte("// ipc") }
func (nopRuntime) WebsocketIPC() []byte     { return []byte("// ws") }
func (nopRuntime) RuntimeDesktopJS() []byte { return []byte("// runtime") }

// The window is served by the wails asset server, not by our handler directly.
// With no Assets fs.FS it must fall through to the handler for the root
// document — otherwise the webview would get wails' default page and never
// reach the dashboard.
func TestWailsAssetServerServesTheBootstrapRedirect(t *testing.T) {
	srv, err := wailsassets.NewAssetServer("", assetserver.Options{
		Handler: bootstrapHandler("http://127.0.0.1:13120/?shell=desktop"),
	}, false, nopLogger{}, nopRuntime{})
	if err != nil {
		t.Fatalf("asset server: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "window.location.replace('http://127.0.0.1:13120/?shell=desktop')") {
		t.Fatalf("the wails asset server did not serve our redirect page:\n%s", body)
	}
}
