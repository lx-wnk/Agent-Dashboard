package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
)

// webhookCGNATBlock covers 100.64.0.0/10 (Carrier-Grade NAT / Tailscale).
var webhookCGNATBlock = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// webhookDialContext re-validates resolved IPs at connection time to prevent DNS
// rebinding attacks (where a domain passes validateWebhookURL but later resolves
// to a private/loopback IP).
func webhookDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("webhook: no IPs resolved for %s", host)
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() ||
			ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
			webhookCGNATBlock.Contains(ip) {
			return nil, fmt.Errorf("webhook: resolved IP %s is blocked (SSRF guard)", ipStr)
		}
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}

// webhookClient is the shared HTTP client used to POST webhook payloads.
// It uses webhookDialContext to guard against DNS rebinding / SSRF at connection time.
var webhookClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: webhookDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("webhook: too many redirects")
		}
		// Strip Authorization on cross-origin redirects.
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	},
}

// validateWebhookURL rejects loopback/private/link-local targets to prevent SSRF.
// Only http and https schemes are allowed.
func validateWebhookURL(raw string) error {
	if raw == "" {
		return nil // empty = no webhook configured, always valid
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook_url must use http or https scheme")
	}
	host := u.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		// Unresolvable host — block it; legitimate webhooks must be DNS-resolvable.
		return fmt.Errorf("webhook_url host could not be resolved: %w", err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("webhook_url must not point to a private or loopback address")
		}
	}
	return nil
}

var validEventTypes = map[string]bool{
	"on_hold":           true,
	"approval_needed":   true,
	"completed":         true,
	"failed":            true,
	"budget_exceeded":   true,
	"iteration_warning": true,
}

const notifPrefPrefix = "notif:pref:"
const notifCfgPrefix = "notif:config:"

type notifPref struct {
	EventType string   `json:"eventType"`
	Channels  []string `json:"channels"`
	Enabled   bool     `json:"enabled"`
}

func (h *Handler) listNotificationPreferences(w http.ResponseWriter, r *http.Request) error {
	all, err := h.cfgRepo.GetAll(r.Context())
	if err != nil {
		return fmt.Errorf("notif_prefs.list: %w", err)
	}
	prefs := make([]notifPref, 0)
	for k, v := range all {
		if !strings.HasPrefix(k, notifPrefPrefix) {
			continue
		}
		et := strings.TrimPrefix(k, notifPrefPrefix)
		var p notifPref
		if err2 := json.Unmarshal([]byte(v), &p); err2 != nil {
			continue
		}
		p.EventType = et
		prefs = append(prefs, p)
	}
	return jsonReply(w, http.StatusOK, prefs)
}

func (h *Handler) putNotificationPreference(w http.ResponseWriter, r *http.Request) error {
	eventType := chi.URLParam(r, "eventType")
	if !validEventTypes[eventType] {
		return apierr.NewAppError(http.StatusBadRequest, "unknown eventType")
	}
	var body struct {
		Channels []string `json:"channels"`
		Enabled  *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	if body.Channels == nil {
		return apierr.NewAppError(http.StatusBadRequest, "channels must be an array")
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	pref := notifPref{EventType: eventType, Channels: body.Channels, Enabled: enabled}
	encoded, _ := json.Marshal(pref)
	if err := h.cfgRepo.Set(r.Context(), notifPrefPrefix+eventType, string(encoded)); err != nil {
		return fmt.Errorf("notif_prefs.put: %w", err)
	}
	return jsonReply(w, http.StatusOK, pref)
}

func (h *Handler) getNotificationConfig(w http.ResponseWriter, r *http.Request) error {
	all, err := h.cfgRepo.GetAll(r.Context())
	if err != nil {
		return fmt.Errorf("notif_config.get: %w", err)
	}
	result := make(map[string]string)
	for k, v := range all {
		if strings.HasPrefix(k, notifCfgPrefix) {
			result[strings.TrimPrefix(k, notifCfgPrefix)] = v
		}
	}
	return jsonReply(w, http.StatusOK, result)
}

func (h *Handler) putNotificationConfig(w http.ResponseWriter, r *http.Request) error {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "invalid JSON body")
	}
	for k, v := range updates {
		var val string
		switch tv := v.(type) {
		case string:
			val = tv
		case nil:
			val = ""
		default:
			val = fmt.Sprintf("%v", tv)
		}
		if k == "webhook_url" {
			if err := validateWebhookURL(val); err != nil {
				return apierr.NewAppError(http.StatusBadRequest, "webhook_url: "+err.Error())
			}
		}
		if err := h.cfgRepo.Set(r.Context(), notifCfgPrefix+k, val); err != nil {
			return fmt.Errorf("notif_config.put: %w", err)
		}
	}
	return h.getNotificationConfig(w, r)
}
