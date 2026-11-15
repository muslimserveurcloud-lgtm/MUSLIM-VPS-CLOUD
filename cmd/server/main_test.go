package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/engine"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

func testRouter(t *testing.T, capacity virtual.Resources) http.Handler {
	t.Helper()

	dir := t.TempDir()
	store, err := virtual.NewStore(filepath.Join(dir, "allocations.json"), capacity)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := engine.NewManager(filepath.Join(dir, "instances.json"), store)
	if err != nil {
		t.Fatal(err)
	}

	return newRouter(store, manager)
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

func TestInstanceLifecycleAPI(t *testing.T) {
	handler := testRouter(t, virtual.Resources{
		VCPU: 8, MemoryMB: 8192, StorageGB: 100,
	})

	body := `{"name":"api-test-vps","resources":{"vcpu":2,"memory_mb":2048,"storage_gb":20}}`
	created := performRequest(handler, http.MethodPost, "/v1/instances", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", created.Code, created.Body.String())
	}

	var instance engine.Instance
	if err := json.Unmarshal(created.Body.Bytes(), &instance); err != nil {
		t.Fatal(err)
	}
	if instance.ID == "" || instance.State != engine.StateStopped {
		t.Fatalf("unexpected instance: %+v", instance)
	}

	listed := performRequest(handler, http.MethodGet, "/v1/instances", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", listed.Code)
	}

	started := performRequest(handler, http.MethodPost, "/v1/instances/"+instance.ID+"/start", "")
	if started.Code != http.StatusOK {
		t.Fatalf("expected 200 for start, got %d: %s", started.Code, started.Body.String())
	}

	got := performRequest(handler, http.MethodGet, "/v1/instances/"+instance.ID, "")
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200 for get, got %d", got.Code)
	}

	restarted := performRequest(handler, http.MethodPost, "/v1/instances/"+instance.ID+"/restart", "")
	if restarted.Code != http.StatusOK {
		t.Fatalf("expected 200 for restart, got %d", restarted.Code)
	}

	stopped := performRequest(handler, http.MethodPost, "/v1/instances/"+instance.ID+"/stop", "")
	if stopped.Code != http.StatusOK {
		t.Fatalf("expected 200 for stop, got %d", stopped.Code)
	}

	deleted := performRequest(handler, http.MethodDelete, "/v1/instances/"+instance.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for delete, got %d: %s", deleted.Code, deleted.Body.String())
	}

	missing := performRequest(handler, http.MethodGet, "/v1/instances/"+instance.ID, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", missing.Code)
	}
}

func TestCannotDeleteRunningInstanceAPI(t *testing.T) {
	handler := testRouter(t, virtual.Resources{
		VCPU: 4, MemoryMB: 4096, StorageGB: 50,
	})

	body := `{"name":"running-vps","resources":{"vcpu":1,"memory_mb":512,"storage_gb":5}}`
	created := performRequest(handler, http.MethodPost, "/v1/instances", body)

	var instance engine.Instance
	if err := json.Unmarshal(created.Body.Bytes(), &instance); err != nil {
		t.Fatal(err)
	}

	started := performRequest(handler, http.MethodPost, "/v1/instances/"+instance.ID+"/start", "")
	if started.Code != http.StatusOK {
		t.Fatalf("expected 200 for start, got %d", started.Code)
	}

	deleted := performRequest(handler, http.MethodDelete, "/v1/instances/"+instance.ID, "")
	if deleted.Code != http.StatusConflict {
		t.Fatalf("expected 409 for deleting running instance, got %d", deleted.Code)
	}
}
