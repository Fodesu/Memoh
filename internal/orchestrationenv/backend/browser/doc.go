// Package browser ships the orchestrationenv.Backend implementation
// for the KindBrowser resource family. It allocates a remote
// Playwright session per env session via the project's existing
// browser gateway (apps/browser, the Bun/Elysia service) and returns
// a runtime handle that carries the WebSocket endpoint and session
// token the worker then uses to drive the browser.
//
// The backend depends on the small Gateway interface defined here so
// tests can swap in a fake without spinning up the real Bun service.
// In production, cmd/agent wiring constructs a concrete HTTPGateway
// pointed at config.BrowserGatewayConfig.
//
// Snapshot is intentionally minimal in this stage: the browser
// gateway has no native snapshot endpoint, so the backend records a
// stable bookkeeping reference (ws endpoint + session id + kind) and
// leaves real cookie/storage/screenshot capture to Stage 3-I when
// drift detection lands.
package browser
