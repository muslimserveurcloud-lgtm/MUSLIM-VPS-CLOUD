package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/engine"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/hypervisor"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

//go:embed web/index.html
var dashboardHTML embed.FS

var version = "1.0.0"

type Response struct {
	Status       string `json:"status"`
	Application  string `json:"application"`
	Version      string `json:"version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Time         string `json:"time"`
}
type apiServer struct {
	store   *virtual.Store
	manager *engine.Manager
}
type allocationRequest struct {
	Name      string            `json:"name"`
	Resources virtual.Resources `json:"resources"`
}
type resourceResponse struct {
	Capacity   virtual.Resources `json:"capacity"`
	Used       virtual.Resources `json:"used"`
	Available  virtual.Resources `json:"available"`
	Configured bool              `json:"configured"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func versionHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, Response{Status: "ready", Application: "MUSLIM VPS CLOUD", Version: version, OS: runtime.GOOS, Architecture: runtime.GOARCH, Time: time.Now().UTC().Format(time.RFC3339)})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var x any
	if err := d.Decode(&x); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
func writeOperationError(w http.ResponseWriter, err error, op string) {
	switch {
	case errors.Is(err, engine.ErrInvalidName), errors.Is(err, virtual.ErrInvalidName), errors.Is(err, virtual.ErrInvalidResources):
		writeError(w, 400, err.Error())
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, virtual.ErrNotFound):
		writeError(w, 404, "instance or resource not found")
	case errors.Is(err, engine.ErrInvalidState):
		writeError(w, 409, err.Error())
	case errors.Is(err, virtual.ErrCapacityNotConfigured):
		writeError(w, 503, err.Error())
	case errors.Is(err, virtual.ErrInsufficientResources):
		writeError(w, 409, err.Error())
	default:
		log.Printf("%s: %v", op, err)
		writeError(w, 500, "operation failed")
	}
}
func (a *apiServer) createAllocation(w http.ResponseWriter, r *http.Request) {
	var q allocationRequest
	if decodeJSON(w, r, &q) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	v, e := a.store.Create(q.Name, q.Resources)
	if e != nil {
		writeOperationError(w, e, "create allocation")
		return
	}
	writeJSON(w, 201, v)
}
func (a *apiServer) listAllocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"allocations": a.store.List()})
}
func (a *apiServer) getAllocation(w http.ResponseWriter, r *http.Request) {
	v, e := a.store.Get(r.PathValue("id"))
	if e != nil {
		writeOperationError(w, e, "get allocation")
		return
	}
	writeJSON(w, 200, v)
}
func (a *apiServer) deleteAllocation(w http.ResponseWriter, r *http.Request) {
	if e := a.store.Delete(r.PathValue("id")); e != nil {
		writeOperationError(w, e, "delete allocation")
		return
	}
	w.WriteHeader(204)
}
func (a *apiServer) resourceUsage(w http.ResponseWriter, r *http.Request) {
	c, u := a.store.Capacity(), a.store.Used()
	writeJSON(w, 200, resourceResponse{Capacity: c, Used: u, Available: virtual.Resources{VCPU: c.VCPU - u.VCPU, MemoryMB: c.MemoryMB - u.MemoryMB, StorageGB: c.StorageGB - u.StorageGB}, Configured: c.VCPU > 0 && c.MemoryMB > 0 && c.StorageGB > 0})
}
func (a *apiServer) createInstance(w http.ResponseWriter, r *http.Request) {
	var q allocationRequest
	if decodeJSON(w, r, &q) != nil {
		writeError(w, 400, "invalid JSON request")
		return
	}
	v, e := a.manager.Create(q.Name, q.Resources)
	if e != nil {
		writeOperationError(w, e, "create instance")
		return
	}
	writeJSON(w, 201, v)
}
func (a *apiServer) listInstances(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"instances": a.manager.List()})
}
func (a *apiServer) getInstance(w http.ResponseWriter, r *http.Request) {
	v, e := a.manager.Get(r.PathValue("id"))
	if e != nil {
		writeOperationError(w, e, "get instance")
		return
	}
	writeJSON(w, 200, v)
}
func (a *apiServer) instanceAction(w http.ResponseWriter, r *http.Request, fn func(string) (engine.Instance, error), op string) {
	v, e := fn(r.PathValue("id"))
	if e != nil {
		writeOperationError(w, e, op)
		return
	}
	writeJSON(w, 200, v)
}
func (a *apiServer) startInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, a.manager.Start, "start")
}
func (a *apiServer) stopInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, a.manager.Stop, "stop")
}
func (a *apiServer) restartInstance(w http.ResponseWriter, r *http.Request) {
	a.instanceAction(w, r, a.manager.Restart, "restart")
}
func (a *apiServer) deleteInstance(w http.ResponseWriter, r *http.Request) {
	if e := a.manager.Delete(r.PathValue("id")); e != nil {
		writeOperationError(w, e, "delete instance")
		return
	}
	w.WriteHeader(204)
}
func newRouter(s *virtual.Store, m *engine.Manager) http.Handler {
	a := &apiServer{store: s, manager: m}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /v1/version", versionHandler)
	mux.HandleFunc("GET /v1/hypervisor", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"mode": m.DriverName()})
	})
	mux.HandleFunc("POST /v1/allocations", a.createAllocation)
	mux.HandleFunc("GET /v1/allocations", a.listAllocations)
	mux.HandleFunc("GET /v1/allocations/{id}", a.getAllocation)
	mux.HandleFunc("DELETE /v1/allocations/{id}", a.deleteAllocation)
	mux.HandleFunc("GET /v1/resources", a.resourceUsage)
	mux.HandleFunc("POST /v1/instances", a.createInstance)
	mux.HandleFunc("GET /v1/instances", a.listInstances)
	mux.HandleFunc("GET /v1/instances/{id}", a.getInstance)
	mux.HandleFunc("POST /v1/instances/{id}/start", a.startInstance)
	mux.HandleFunc("POST /v1/instances/{id}/stop", a.stopInstance)
	mux.HandleFunc("POST /v1/instances/{id}/restart", a.restartInstance)
	mux.HandleFunc("DELETE /v1/instances/{id}", a.deleteInstance)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeFileFS(w, r, dashboardHTML, "web/index.html")
		})
		return mux
}
func envInt(k string) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return 0, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", k)
	}
	return n, nil
}
func main() {
	c := virtual.Resources{}
	var err error
	if c.VCPU, err = envInt("MSC_CAPACITY_VCPU"); err != nil {
		log.Fatal(err)
	}
	if c.MemoryMB, err = envInt("MSC_CAPACITY_MEMORY_MB"); err != nil {
		log.Fatal(err)
	}
	if c.StorageGB, err = envInt("MSC_CAPACITY_STORAGE_GB"); err != nil {
		log.Fatal(err)
	}
	sf := os.Getenv("MSC_STATE_FILE")
	if sf == "" {
		sf = "data/allocations.json"
	}
	store, err := virtual.NewStore(sf, c)
	if err != nil {
		log.Fatalf("Virtual Core: %v", err)
	}
	ef := os.Getenv("MSC_ENGINE_STATE_FILE")
	if ef == "" {
		ef = "data/instances.json"
	}
	mode := strings.ToLower(os.Getenv("MSC_HYPERVISOR"))
	var driver hypervisor.Driver
	switch mode {
	case "", "simulation", "none":
		mode = "simulation"
	case "kvm":
		driver, err = hypervisor.NewKVMDriver(hypervisor.KVMConfig{URI: os.Getenv("MSC_KVM_URI"), StorageDir: os.Getenv("MSC_KVM_STORAGE_DIR"), BaseImage: os.Getenv("MSC_KVM_BASE_IMAGE"), BaseImageFormat: os.Getenv("MSC_KVM_BASE_IMAGE_FORMAT"), Network: os.Getenv("MSC_KVM_NETWORK"), VirshPath: os.Getenv("MSC_KVM_VIRSH_PATH"), QemuImgPath: os.Getenv("MSC_KVM_QEMU_IMG_PATH")}, nil)
		if err != nil {
			log.Fatalf("KVM configuration: %v", err)
		}
	default:
		log.Fatalf("MSC_HYPERVISOR must be simulation or kvm")
	}
	manager, err := engine.NewManagerWithDriver(ef, store, driver)
	if err != nil {
		log.Fatalf("VPS Engine: %v", err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: newRouter(store, manager), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("MUSLIM VPS CLOUD listening on :%s (hypervisor=%s)", port, manager.DriverName())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
