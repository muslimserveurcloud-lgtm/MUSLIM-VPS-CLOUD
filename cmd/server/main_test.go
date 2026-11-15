package main

import (
"net/http"
"net/http/httptest"
"testing"
)

func TestEndpoints(t *testing.T) {
tests := []struct {
path string
}{
{"/health"},
{"/v1/version"},
{"/"},
}

mux := http.NewServeMux()
mux.HandleFunc("/health", health)
mux.HandleFunc("/v1/version", versionHandler)
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
writeJSON(w, http.StatusOK, map[string]string{
"application": "MUSLIM VPS CLOUD",
})
})

for _, test := range tests {
t.Run(test.path, func(t *testing.T) {
req := httptest.NewRequest("GET", test.path, nil)
rec := httptest.NewRecorder()

mux.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("expected 200, got %d", rec.Code)
}
})
}
}
