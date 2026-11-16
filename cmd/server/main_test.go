package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/engine"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

func TestHealthAndModeEndpoints(t *testing.T) {
	d := t.TempDir()
	s, e := virtual.NewStore(filepath.Join(d, "alloc.json"), virtual.Resources{})
	if e != nil {
		t.Fatal(e)
	}
	m, e := engine.NewManager(filepath.Join(d, "instances.json"), s)
	if e != nil {
		t.Fatal(e)
	}
	h := newRouter(s, m)
	for _, p := range []string{"/health", "/v1/version", "/v1/hypervisor"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, p, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}
}
