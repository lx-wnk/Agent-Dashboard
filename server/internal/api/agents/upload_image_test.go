package agents

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tinyPNG is a 1x1 transparent PNG, small enough to embed directly.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// newUploadRequest builds a multipart POST /api/agents/{pid}/upload-image
// request with a single "image" field carrying content under filename with
// the given contentType.
func newUploadRequest(t *testing.T, pid, filename, contentType string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="image"; filename="` + filename + `"`}
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	part, err := w.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+pid+"/upload-image", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.SetPathValue("pid", pid)
	return req
}

func TestUploadImage_ValidImage_SavesUnderTempBase(t *testing.T) {
	h := NewUploadImageHandler()
	req := newUploadRequest(t, "4242", "avatar.png", "image/png", tinyPNG)
	rec := httptest.NewRecorder()

	h.UploadImage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	t.Cleanup(func() { _ = os.Remove(body.Path) })

	require.NotEmpty(t, body.Path)
	assert.True(t, filepath.IsAbs(body.Path), "path must be absolute, got %q", body.Path)

	base := filepath.Join(os.TempDir(), "agent-dashboard-uploads")
	assert.True(t, strings.HasPrefix(body.Path, base), "expected %q to be under %q", body.Path, base)
	assert.Contains(t, body.Path, "4242")
	assert.Equal(t, ".png", filepath.Ext(body.Path))

	info, err := os.Stat(body.Path)
	require.NoError(t, err)
	assert.Equal(t, int64(len(tinyPNG)), info.Size())
}

func TestUploadImage_RejectsNonImageContentType(t *testing.T) {
	h := NewUploadImageHandler()
	req := newUploadRequest(t, "111", "notes.txt", "text/plain", []byte("hello world"))
	rec := httptest.NewRecorder()

	h.UploadImage(rec, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestUploadImage_RejectsOversizedBody(t *testing.T) {
	h := NewUploadImageHandler()
	oversized := bytes.Repeat([]byte{0xff}, maxUploadImageBytes+1)
	req := newUploadRequest(t, "222", "big.png", "image/png", oversized)
	rec := httptest.NewRecorder()

	h.UploadImage(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestUploadImage_InvalidPID(t *testing.T) {
	h := NewUploadImageHandler()
	req := newUploadRequest(t, "not-a-number", "avatar.png", "image/png", tinyPNG)
	rec := httptest.NewRecorder()

	h.UploadImage(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadImage_MissingFileField(t *testing.T) {
	h := NewUploadImageHandler()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/agents/333/upload-image", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.SetPathValue("pid", "333")
	rec := httptest.NewRecorder()

	h.UploadImage(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
