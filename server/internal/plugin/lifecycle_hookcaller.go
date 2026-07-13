package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPHookCaller POSTs hooks to the plugin's Addr. Reachability (process running)
// is SP2's concern; here a non-2xx or transport error fails the transition.
type HTTPHookCaller struct{ client *http.Client }

func NewHTTPHookCaller() *HTTPHookCaller {
	return &HTTPHookCaller{client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *HTTPHookCaller) Call(ctx context.Context, d Descriptor, hook string) error {
	url := "http://" + d.Addr + hook
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("hook %s unreachable: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hook %s returned %d", url, resp.StatusCode)
	}
	return nil
}
