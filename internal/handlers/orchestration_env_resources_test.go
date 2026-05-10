package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/memohai/memoh/internal/orchestrationenv"
)

type fakeEnvResourceAPI struct {
	registered  []orchestrationenv.RegisterResourceRequest
	updated     []orchestrationenv.UpdateResourceRequest
	getCalls    []string
	listCalls   []string
	deleteCalls []string
	registerOut *orchestrationenv.Resource
	updateOut   *orchestrationenv.Resource
	getOut      *orchestrationenv.Resource
	listOut     []orchestrationenv.Resource
	registerErr error
	updateErr   error
	getErr      error
	listErr     error
	deleteErr   error
}

func (f *fakeEnvResourceAPI) RegisterResource(_ context.Context, req orchestrationenv.RegisterResourceRequest) (*orchestrationenv.Resource, error) {
	f.registered = append(f.registered, req)
	return f.registerOut, f.registerErr
}

func (f *fakeEnvResourceAPI) UpdateResource(_ context.Context, req orchestrationenv.UpdateResourceRequest) (*orchestrationenv.Resource, error) {
	f.updated = append(f.updated, req)
	return f.updateOut, f.updateErr
}

func (f *fakeEnvResourceAPI) DeleteResource(_ context.Context, id string) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return f.deleteErr
}

func (f *fakeEnvResourceAPI) GetResource(_ context.Context, id string) (*orchestrationenv.Resource, error) {
	f.getCalls = append(f.getCalls, id)
	return f.getOut, f.getErr
}

func (f *fakeEnvResourceAPI) ListResources(_ context.Context, tenantID string) ([]orchestrationenv.Resource, error) {
	f.listCalls = append(f.listCalls, tenantID)
	return f.listOut, f.listErr
}

func setEnvResourceUserToken(c echo.Context, tenantID, userID string) {
	c.Set("user", &jwt.Token{
		Valid: true,
		Claims: jwt.MapClaims{
			"tenant_id": tenantID,
			"user_id":   userID,
		},
	})
}

func TestEnvResourceHandlerRegistersRoutes(t *testing.T) {
	e := echo.New()
	handler := NewEnvResourceHandler(slog.New(slog.DiscardHandler), nil)
	handler.Register(e)

	want := map[string]string{
		http.MethodGet + " " + "/orchestration/env-resources":        "",
		http.MethodPost + " " + "/orchestration/env-resources":       "",
		http.MethodGet + " " + "/orchestration/env-resources/:id":    "",
		http.MethodPatch + " " + "/orchestration/env-resources/:id":  "",
		http.MethodDelete + " " + "/orchestration/env-resources/:id": "",
	}
	for _, route := range e.Routes() {
		key := route.Method + " " + route.Path
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("Register() missing routes: %+v", want)
	}
}

func TestEnvResourceHandlerListEnvResourcesReturnsEmptyWhenManagerMissing(t *testing.T) {
	handler := NewEnvResourceHandler(slog.New(slog.DiscardHandler), nil)
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/orchestration/env-resources", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setEnvResourceUserToken(c, "tenant-1", "user-1")

	if err := handler.ListEnvResources(c); err != nil {
		t.Fatalf("ListEnvResources() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("ListEnvResources() status = %d, want 200", rec.Code)
	}
	var page EnvResourceListPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("ListEnvResources() items = %d, want 0", len(page.Items))
	}
}

func TestEnvResourceHandlerListEnvResourcesScopesByCallerTenant(t *testing.T) {
	now := time.Now().UTC()
	api := &fakeEnvResourceAPI{
		listOut: []orchestrationenv.Resource{
			{ID: "r-1", TenantID: "tenant-1", Kind: orchestrationenv.KindContainer, Name: "alpine", Capacity: 4, Status: orchestrationenv.ResourceStatusActive, CreatedAt: now, UpdatedAt: now},
			{ID: "r-2", TenantID: "tenant-1", Kind: orchestrationenv.KindBrowser, Name: "chromium", Capacity: 2, Status: orchestrationenv.ResourceStatusActive, CreatedAt: now, UpdatedAt: now},
		},
	}
	handler := newEnvResourceHandlerWithAPI(api)

	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/orchestration/env-resources", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setEnvResourceUserToken(c, "tenant-1", "user-1")

	if err := handler.ListEnvResources(c); err != nil {
		t.Fatalf("ListEnvResources() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("ListEnvResources() status = %d, want 200", rec.Code)
	}
	if len(api.listCalls) != 1 || api.listCalls[0] != "tenant-1" {
		t.Fatalf("ListResources() tenant calls = %+v, want [tenant-1]", api.listCalls)
	}
	var page EnvResourceListPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items count = %d, want 2", len(page.Items))
	}
}

func TestEnvResourceHandlerRegisterEnvResourceForwardsCallerTenantAndSubject(t *testing.T) {
	now := time.Now().UTC()
	api := &fakeEnvResourceAPI{
		registerOut: &orchestrationenv.Resource{
			ID: "r-3", TenantID: "tenant-9", OwnerSubject: "user-9", Kind: orchestrationenv.KindContainer,
			Name: "build-shell", Capacity: 8, Status: orchestrationenv.ResourceStatusActive, CreatedAt: now, UpdatedAt: now,
		},
	}
	handler := newEnvResourceHandlerWithAPI(api)

	body := `{"kind":"container","name":"build-shell","capacity":8,"config":{"image":"alpine:3.20"},"metadata":{"region":"local"}}`
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orchestration/env-resources", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setEnvResourceUserToken(c, "tenant-9", "user-9")

	if err := handler.RegisterEnvResource(c); err != nil {
		t.Fatalf("RegisterEnvResource() error = %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("RegisterEnvResource() status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(api.registered) != 1 {
		t.Fatalf("RegisterResource() call count = %d, want 1", len(api.registered))
	}
	got := api.registered[0]
	if got.TenantID != "tenant-9" || got.OwnerSubject != "user-9" {
		t.Fatalf("RegisterResource() identity = (%s, %s), want (tenant-9, user-9)", got.TenantID, got.OwnerSubject)
	}
	if got.Name != "build-shell" || got.Kind != orchestrationenv.KindContainer || got.Capacity != 8 {
		t.Fatalf("RegisterResource() request = %+v", got)
	}
	if got.Config["image"] != "alpine:3.20" {
		t.Fatalf("RegisterResource() config image = %v, want alpine:3.20", got.Config["image"])
	}
}

func TestEnvResourceHandlerRegisterEnvResourceMapsInvalidArgumentTo400(t *testing.T) {
	api := &fakeEnvResourceAPI{registerErr: orchestrationenv.ErrInvalidArgument}
	handler := newEnvResourceHandlerWithAPI(api)

	body := `{"kind":"container","name":"x"}`
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orchestration/env-resources", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setEnvResourceUserToken(c, "tenant-1", "user-1")

	err := handler.RegisterEnvResource(c)
	if err == nil {
		t.Fatal("RegisterEnvResource() error = nil, want HTTPError 400")
	}
	httpErr := &echo.HTTPError{}
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("RegisterEnvResource() error = %v, want 400", err)
	}
}

func TestEnvResourceHandlerGetEnvResourceHidesForeignTenantAs404(t *testing.T) {
	now := time.Now().UTC()
	api := &fakeEnvResourceAPI{
		getOut: &orchestrationenv.Resource{
			ID: "r-7", TenantID: "tenant-other", Kind: orchestrationenv.KindContainer, Name: "shared",
			Capacity: 1, Status: orchestrationenv.ResourceStatusActive, CreatedAt: now, UpdatedAt: now,
		},
	}
	handler := newEnvResourceHandlerWithAPI(api)

	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/orchestration/env-resources/r-7", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("r-7")
	setEnvResourceUserToken(c, "tenant-1", "user-1")

	err := handler.GetEnvResource(c)
	httpErr := &echo.HTTPError{}
	if err == nil {
		t.Fatal("GetEnvResource() error = nil, want 404 for foreign tenant")
	}
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusNotFound {
		t.Fatalf("GetEnvResource() error = %v, want 404", err)
	}
}

func TestEnvResourceHandlerUpdateEnvResourceForwardsFields(t *testing.T) {
	now := time.Now().UTC()
	api := &fakeEnvResourceAPI{
		getOut: &orchestrationenv.Resource{
			ID: "r-9", TenantID: "tenant-1", Kind: orchestrationenv.KindContainer, Name: "shared",
			Capacity: 4, Status: orchestrationenv.ResourceStatusActive, CreatedAt: now, UpdatedAt: now,
		},
		updateOut: &orchestrationenv.Resource{
			ID: "r-9", TenantID: "tenant-1", Kind: orchestrationenv.KindContainer, Name: "shared",
			Capacity: 6, Status: orchestrationenv.ResourceStatusDisabled, CreatedAt: now, UpdatedAt: now,
		},
	}
	handler := newEnvResourceHandlerWithAPI(api)

	body := `{"capacity":6,"status":"disabled","config":{"image":"alpine:3.21"}}`
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/orchestration/env-resources/r-9", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("r-9")
	setEnvResourceUserToken(c, "tenant-1", "user-1")

	if err := handler.UpdateEnvResource(c); err != nil {
		t.Fatalf("UpdateEnvResource() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateEnvResource() status = %d, want 200", rec.Code)
	}
	if len(api.updated) != 1 {
		t.Fatalf("UpdateResource() call count = %d, want 1", len(api.updated))
	}
	got := api.updated[0]
	if got.ID != "r-9" || got.Capacity != 6 || got.Status != orchestrationenv.ResourceStatusDisabled {
		t.Fatalf("UpdateResource() request = %+v", got)
	}
	if got.Config["image"] != "alpine:3.21" {
		t.Fatalf("UpdateResource() config image = %v, want alpine:3.21", got.Config["image"])
	}
}

func newEnvResourceHandlerWithAPI(api envResourceAPI) *EnvResourceHandler {
	return &EnvResourceHandler{
		api:    api,
		logger: slog.New(slog.DiscardHandler),
	}
}
