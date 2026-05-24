# Lessons Learned

<!-- Format: - **[scope]** What happened — what we learned `conf:high|med|low` (YYYY-MM-DD) -->
<!-- conf:high = proven 3+ times | conf:med = observed once/twice | conf:low = suspected -->
<!-- The date enables staleness tracking. Always include it. -->

- **[security/xss]** G-C SEC-11 audit completed (PR security/sec11-vhtml-owasp) — only 3 `v-html` call sites exist (`AgentChatStream.vue` ×2, `RefinementChat.vue` ×1), all rendering user-controlled markdown via the SSOT `src/utils/markdown.ts` chokepoint which already pipes through DOMPurify. New `src/composables/useSafeHtml.ts` covers any future direct-HTML sinks. OWASP A03: all DB access is parameterized (incl. FTS5 search via quoted-token sanitizer in `server/internal/api/search/handler.go`). OWASP A07: JWT/OAuth-state cookies are `HttpOnly`, `Secure` (non-loopback), `SameSite=Strict|Lax`; `DASHBOARD_JWT_SECRET` requires ≥32 chars. OWASP A08: plugin loader `server/internal/plugin/registry.go` is wildcard — loads any directory containing a valid `plugin.json` with no enabled-list or hash pin; loopback-bind + plugin-id regex are the current safeguards. Documented in PR body; hardening deferred to a follow-up. `conf:high` (2026-05-24)
