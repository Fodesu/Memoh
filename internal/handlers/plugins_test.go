package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestPluginsHandlerRegisterDoesNotExposeRemovedRoutes(t *testing.T) {
	e := echo.New()
	(&PluginsHandler{}).Register(e)

	tests := []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodPost, path: "/bots/bot-1/plugins", status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/bots/bot-1/plugins/plugin-1/oauth/authorize", status: http.StatusNotFound},
		{method: http.MethodGet, path: "/bots/bot-1/plugins/plugin-1/oauth/status", status: http.StatusNotFound},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, rec.Code, test.status)
		}
	}
}
