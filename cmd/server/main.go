package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

var version = "development"

type Response struct {
	Status       string `json:"status"`
	Application  string `json:"application"`
	Version      string `json:"version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Time         string `json:"time"`
}

type apiServer struct {
	store *virtual.Store
}

type allocationRequest struct {
	Name      string            `json:"name"`
	Resources virtual.Resources `json:"resources"`
}

type resourceResponse struct {
	Capacity  virtual.Resources `json:"capacity"`
	Used      virtual.Resources `json:"used"`
	Available virtual.Resources `json:"available"`
	Configured bool              `json:"configured"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{
		Status:       "ready",
		Application:  "MUSLIM VPS CLOUD",
		Version:      version,
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		Time:         time.Now().UTC().Format(time.RFC3339),
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request must contain only one JSON value")
		}
		return err
	}
	return nil
}

func (a *apiServer) createAllocation(w http.ResponseWriter, r *http.Request) {
	var request allocationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}

	allocation, err := a.store.Create(request.Name, request.Resources)
	if err != nil {
		switch {
		case errors.Is(err, virtual.ErrInvalidName),
			errors.Is(err, virtual.ErrInvalidResources):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, virtual.ErrCapacityNotConfigured):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, virtual.ErrInsufficientResources):
			writeError(w, http.StatusConflict, err.Error())
		default:
			log.Printf("create allocation: %v", err)
			writeError(w, http.StatusInternalServerError, "could not create allocation")
		}
		return
	}

	writeJSON(w, http.StatusCreated, allocation)
}

func (a *apiServer) listAllocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"allocations": a.store.List(),
	})
}

func (a *apiServer) getAllocation(w http.ResponseWriter, r *http.Request) {
	allocation, err := a.store.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, virtual.ErrNotFound) {
			writeError(w, http.StatusNotFound, "allocation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not read allocation")
		return
	}
	writeJSON(w, http.StatusOK, allocation)
}

func (a *apiServer) deleteAllocation(w http.ResponseWriter, r *http.Request) {
	err := a.store.Delete(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, virtual.ErrNotFound) {
			writeError(w, http.StatusNotFound, "allocation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete allocation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *apiServer) resourceUsage(w http.ResponseWriter, r *http.Request) {
	capacity := a.store.Capacity()
	used := a.store.Used()

	available := virtual.Resources{
		VCPU:      capacity.VCPU - used.VCPU,
		MemoryMB:  capacity.MemoryMB - used.MemoryMB,
		StorageGB: capacity.StorageGB - used.StorageGB,
	}

	configured := capacity.VCPU > 0 &&
		capacity.MemoryMB > 0 &&
		capacity.StorageGB > 0

	writeJSON(w, http.StatusOK, resourceResponse{
		Capacity:   capacity,
		Used:       used,
		Available:  available,
		Configured: configured,
	})
}

func newRouter(store *virtual.Store) http.Handler {
	api := &apiServer{store: store}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /v1/version", versionHandler)
	mux.HandleFunc("POST /v1/allocations", api.createAllocation)
	mux.HandleFunc("GET /v1/allocations", api.listAllocations)
	mux.HandleFunc("GET /v1/allocations/{id}", api.getAllocation)
	mux.HandleFunc("DELETE /v1/allocations/{id}", api.deleteAllocation)
	mux.HandleFunc("GET /v1/resources", api.resourceUsage)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"application": "MUSLIM VPS CLOUD",
			"message":     "Cloud infrastructure API is running",
			"health":      "/health",
			"version":     "/v1/version",
		})
	})

	return mux
}

func envInt(name string) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, nil
	}

	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return number, nil
}

func main() {
	capacity := virtual.Resources{}
	var err error

	if capacity.VCPU, err = envInt("MSC_CAPACITY_VCPU"); err != nil {
		log.Fatal(err)
	}
	if capacity.MemoryMB, err = envInt("MSC_CAPACITY_MEMORY_MB"); err != nil {
		log.Fatal(err)
	}
	if capacity.StorageGB, err = envInt("MSC_CAPACITY_STORAGE_GB"); err != nil {
		log.Fatal(err)
	}

	stateFile := os.Getenv("MSC_STATE_FILE")
	if stateFile == "" {
		stateFile = "data/allocations.json"
	}

	store, err := virtual.NewStore(stateFile, capacity)
	if err != nil {
		log.Fatalf("initialize Virtual Core: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(store),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("MUSLIM VPS CLOUD API listening on :%s", port)
	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
