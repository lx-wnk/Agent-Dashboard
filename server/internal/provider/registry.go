package provider

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"gopkg.in/yaml.v3"
)

// Pricing is the seam to the pricing table (parser/pricing wrappers). Defined
// as an interface so the registry stays testable without the real table.
type Pricing interface {
	HasPricing(model string) bool
	EstimateCost(u sdk.TokenUsage, model string) float64
	EstimateCacheCreationCost(u sdk.TokenUsage, model string) float64
	EstimateCacheReadCost(u sdk.TokenUsage, model string) float64
}

// CostBreakdown mirrors merger.CostBreakdown plus a Local flag for $0 local models.
type CostBreakdown struct {
	Total       float64
	CacheCreate float64
	CacheRead   float64
	Unknown     bool
	Local       bool
}

type Options struct {
	UserDir   string
	EnabledFn EnabledFunc
	Ollama    *OllamaClassifier
	Pricing   Pricing
}

type Registry struct {
	descriptors map[string]Descriptor
	byExe       map[string]string
	enabled     EnabledFunc
	ollama      *OllamaClassifier
	pricing     Pricing
}

func NewRegistry(opt Options) (*Registry, error) {
	r := &Registry{
		descriptors: map[string]Descriptor{},
		byExe:       map[string]string{},
		enabled:     opt.EnabledFn,
		ollama:      opt.Ollama,
		pricing:     opt.Pricing,
	}
	if r.enabled == nil {
		r.enabled = func(string) bool { return false }
	}
	if err := r.loadFS(builtinFS, "providers"); err != nil {
		return nil, err
	}
	if opt.UserDir != "" {
		if entries, err := os.ReadDir(opt.UserDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				r.loadFile(filepath.Join(opt.UserDir, e.Name()))
			}
		}
	}
	return r, nil
}

func (r *Registry) loadFS(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("provider.loadFS: %w", err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(fsys, filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		r.ingest(b, e.Name())
	}
	return nil
}

func (r *Registry) loadFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r.ingest(b, path)
}

// ingest decodes, validates, and registers one descriptor. A bad descriptor is
// logged and dropped — never fatal.
func (r *Registry) ingest(b []byte, name string) {
	var d Descriptor
	if err := yaml.Unmarshal(b, &d); err != nil {
		slog.Warn("provider descriptor parse failed", "file", name, "err", err)
		return
	}
	if err := d.Validate(); err != nil {
		slog.Warn("provider descriptor invalid", "file", name, "err", err)
		return
	}
	r.descriptors[d.ID] = d
	for _, exe := range d.ExeNames {
		r.byExe[exe] = d.ID
	}
}

func (r *Registry) Descriptors() map[string]Descriptor { return r.descriptors }
func (r *Registry) SetEnabled(fn EnabledFunc)          { r.enabled = fn }

// ProviderInfo is the public, UI-facing summary of a known provider.
type ProviderInfo struct {
	ID               string
	DisplayName      string
	ConfigDirPresent bool
}

// KnownProviders returns every loaded descriptor (sorted by id) with its
// display name and whether its config directory exists on disk. Claude is the
// always-on built-in and is intentionally excluded (it is not a descriptor).
func (r *Registry) KnownProviders() []ProviderInfo {
	home, _ := os.UserHomeDir()
	ids := make([]string, 0, len(r.descriptors))
	for id := range r.descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ProviderInfo, 0, len(ids))
	for _, id := range ids {
		d := r.descriptors[id]
		dir := expandHome(d.ConfigDir.Default, home)
		if d.ConfigDir.Env != "" {
			if v := os.Getenv(d.ConfigDir.Env); v != "" {
				dir = v
			}
		}
		out = append(out, ProviderInfo{
			ID:               id,
			DisplayName:      d.DisplayName,
			ConfigDirPresent: dir != "" && isDir(dir),
		})
	}
	return out
}

// DetectProvider maps a process command to an enabled provider id, or "".
func (r *Registry) DetectProvider(comm string) sdk.Provider {
	comm = strings.TrimSpace(comm)
	if comm == "" {
		return ""
	}
	if i := strings.IndexByte(comm, ' '); i >= 0 {
		comm = comm[:i]
	}
	base := filepath.Base(comm)
	if base == "claude" {
		return sdk.ProviderClaude
	}
	id, ok := r.byExe[base]
	if !ok || !r.enabled(id) {
		return ""
	}
	return sdk.Provider(id)
}

// ConfigDirs returns existing config dirs for all enabled jsonl providers.
func (r *Registry) ConfigDirs() []parser.ProviderConfigDir {
	home, _ := os.UserHomeDir()
	var out []parser.ProviderConfigDir
	ids := make([]string, 0, len(r.descriptors))
	for id := range r.descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := r.descriptors[id]
		if !r.enabled(id) || d.IsCustom() {
			continue
		}
		dir := expandHome(d.ConfigDir.Default, home)
		if d.ConfigDir.Env != "" {
			if v := os.Getenv(d.ConfigDir.Env); v != "" {
				dir = v
			}
		}
		if dir != "" && isDir(dir) {
			out = append(out, parser.ProviderConfigDir{Provider: sdk.Provider(id), Path: dir})
		}
	}
	return out
}

// ResolveSession finds and parses the newest session file for a non-Claude
// provider under cwd. claimed excludes already-bound session ids.
func (r *Registry) ResolveSession(p sdk.Provider, cwd string, claimed map[string]bool) (*parser.SessionData, string, float64, error) {
	d, ok := r.descriptors[string(p)]
	if !ok || d.IsCustom() {
		return nil, "", 0, fmt.Errorf("no jsonl descriptor for %s", p)
	}
	for _, pcd := range r.ConfigDirs() {
		if pcd.Provider != p {
			continue
		}
		matches := findSessions(pcd.Path, d.SessionGlob)
		sort.Slice(matches, func(i, j int) bool {
			return fileMtime(matches[i]).After(fileMtime(matches[j]))
		})
		for _, path := range matches {
			id := sessionID(d, path)
			if claimed != nil && claimed[id] {
				continue
			}
			res, err := parseJSONL(d, path)
			if err != nil {
				continue
			}
			res.Session.SessionID = id
			res.Session.ProjectPath = cwd
			res.Session.Path = path
			if claimed != nil {
				claimed[id] = true
			}
			return res.Session, res.Provider, res.InFileCost, nil
		}
	}
	return nil, "", 0, fmt.Errorf("no %s session for %s", p, cwd)
}

// Cost computes the cost breakdown for a provider session, honoring localIf
// ($0), in-file cost, and the pricing table.
func (r *Registry) Cost(p sdk.Provider, usage sdk.TokenUsage, model string, inFileCost float64, inFileProvider string) CostBreakdown {
	d, ok := r.descriptors[string(p)]
	if !ok {
		return CostBreakdown{
			Total:       r.pricing.EstimateCost(usage, model),
			CacheCreate: r.pricing.EstimateCacheCreationCost(usage, model),
			CacheRead:   r.pricing.EstimateCacheReadCost(usage, model),
		}
	}
	if d.Cost.LocalIf != nil && r.ollama != nil {
		li := d.Cost.LocalIf
		if r.ollama.IsLocal(inFileProvider, model) ||
			(li.ProviderEquals != "" && strings.EqualFold(inFileProvider, li.ProviderEquals)) {
			return CostBreakdown{Local: true}
		}
	}
	switch d.Cost.Rule {
	case CostInFile:
		return CostBreakdown{Total: inFileCost}
	case CostNone:
		return CostBreakdown{Unknown: true}
	default:
		if !r.pricing.HasPricing(model) {
			return CostBreakdown{Unknown: true}
		}
		return CostBreakdown{
			Total:       r.pricing.EstimateCost(usage, model),
			CacheCreate: r.pricing.EstimateCacheCreationCost(usage, model),
			CacheRead:   r.pricing.EstimateCacheReadCost(usage, model),
		}
	}
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileMtime(p string) time.Time {
	if info, err := os.Stat(p); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// sessionID derives a session id from a session-file path per the descriptor's
// sessionIdFrom: "parentDir" uses the containing directory name (for providers
// with a fixed session filename like junie's events.jsonl); default uses the
// filename without the .jsonl suffix.
func sessionID(d Descriptor, path string) string {
	if d.SessionIDFrom == "parentDir" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// globToRegexp compiles a path glob into an anchored regexp. "**" matches any
// run of characters including "/"; "*" matches within a path segment; "?"
// matches a single non-separator char. A "**/" prefix also matches zero dirs.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++ // consume second '*'
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++ // consume '/', so "**/x" also matches "x"
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '[', ']', '{', '}', '^', '$', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

// findSessions returns every regular file under root whose root-relative,
// slash-separated path matches the glob (with ** support).
func findSessions(root, glob string) []string {
	re, err := globToRegexp(glob)
	if err != nil {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if re.MatchString(filepath.ToSlash(rel)) {
			out = append(out, path)
		}
		return nil
	})
	return out
}
