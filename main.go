package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg := apiConfig{fileserverHits: atomic.Int32{}}
	mux := http.NewServeMux()
	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", cfg.handleMetrics)
	mux.HandleFunc("POST /admin/reset", cfg.handleReset)
	mux.HandleFunc("POST /api/validate_chirp", handleValidate)
	mux.HandleFunc("GET /api/healthz", handleHealthz)

	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Fatal(server.ListenAndServe())
}

type validationPayload struct {
	Body string `json:"body"`
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	payload := validationPayload{}
	err := decoder.Decode(&payload)
	if err != nil {
		handleError(w, http.StatusBadRequest, "malformed request")
		return
	}

	if len(payload.Body) > 140 {
		handleError(w, http.StatusBadRequest, "Chirp too long")
		return
	}

	resp, _ := json.Marshal(struct {
		Valid bool `json:"valid"`
	}{Valid: true})
	w.Write(resp)
}

func handleError(w http.ResponseWriter, statusCode int, err string) {
	w.WriteHeader(statusCode)
	resp, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: err})
	w.Write(resp)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

func (cfg *apiConfig) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	hits := cfg.fileserverHits.Load()
	fmt.Fprintf(w,
		`<html>
  		<body>
    		<h1>Welcome, Chirpy Admin</h1>
    		<p>Chirpy has been visited %d times!</p>
  		</body>
		</html>
		`, hits)
}

func (cfg *apiConfig) handleReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cfg.fileserverHits.Store(0)
	fmt.Fprintf(w, "Hits reset\n")
}
