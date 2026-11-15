package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

func testRouter(t *testing.T, capacity virtual.Resources) http.Handler {
	t.Helper()

	store, err := virtual.NewStore(
		filepath.Join(t.TempDir(), "allocations.json"),
		capacity,
	)
	if err != nil {
		t.Fatal(err)
	}
	return newRouter(store)
}

func performRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestExistingEndpoints(t *testing.T) {
	handler := testRouter(t, virtual.Resources{})

	for _, path := range []string{"/health", "/v1/version", "/"} {
		t.Run(path, func(t *testing.T) {
			rec := performRequest(handler, http.MethodGet, path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
		})
	}
}

func TestAllocationLifecycle(t *testing.T) {
	handler := testRouter(t, virtual.Resources{
		VCPU: 8, MemoryMB: 8192, StorageGB: 100,
	})

	body := `{"name":"test-vps","resources":{"vcpu":2,"memory_mb":2048,"storage_gb":20}}`
	created := performRequest(handler, http.MethodPost, "/v1/allocations", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", created.Code, created.Body.String())
	}

	var allocation virtual.Allocation
	if err := json.Unmarshal(created.Body.Bytes(), &allocation); err != nil {
		t.Fatal(err)
	}
	if allocation.ID == "" {
		t.Fatal("expected allocation ID")
	}

	listed := performRequest(handler, http.MethodGet, "/v1/allocations", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", listed.Code)
	}

	got := performRequest(handler, http.MethodGet, "/v1/allocations/"+allocation.ID, "")
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200 for get, got %d", got.Code)
	}

	deleted := performRequest(handler, http.MethodDelete, "/v1/allocations/"+allocation.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for delete, got %d", deleted.Code)
	}

	missing := performRequest(handler, http.MethodGet, "/v1/allocations/"+allocation.ID, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", missing.Code)
	}
}

func TestResourceUsage(t *testing.T) {
	handler := testRouter(t, virtual.Resources{
		VCPU: 4, MemoryMB: 4096, StorageGB: 50,
	})

	rec := performRequest(handler, http.MethodGet, "/v1/resources", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result resourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Configured || result.Available.VCPU != 4 {
		t.Fatalf("unexpected resource response: %+v", result)
	}
}

func TestAllocationRejectedWithoutCapacity(t *testing.T) {
	handler := testRouter(t, virtual.Resources{})

	body := `{"name":"test-vps","resources":{"vcpu":1,"memory_mb":512,"storage_gb":5}}`
	rec := performRequest(handler, http.MethodPost, "/v1/allocations", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
