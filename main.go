package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/willr42/chirpy/internal/database"
)

type apiConfig struct {
	db             *database.Queries
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func main() {
	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("could not open db at %s", dbURL)
	}

	dbQueries := database.New(db)

	cfg := apiConfig{fileserverHits: atomic.Int32{}, db: dbQueries}
	mux := http.NewServeMux()
	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", cfg.handleMetrics)
	mux.Handle("POST /admin/reset", checkEnv(http.HandlerFunc(cfg.handleReset)))
	mux.HandleFunc("POST /api/users", cfg.handleCreateUser)
	mux.HandleFunc("GET /api/healthz", handleHealthz)
	mux.HandleFunc("GET /api/chirps", cfg.handleGetAllChirps)
	mux.HandleFunc("GET /api/chirps/{chirpId}", cfg.handleGetChirp)
	mux.HandleFunc("POST /api/chirps", cfg.handleCreateChirp)

	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Fatal(server.ListenAndServe())
}

type chirpPayload struct {
	Body   string    `json:"body"`
	UserId uuid.UUID `json:"user_id"`
}

type chirp struct {
	Id        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

func (cfg *apiConfig) handleCreateChirp(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	payload := chirpPayload{}
	err := decoder.Decode(&payload)
	if err != nil {
		handleError(w, http.StatusBadRequest, "malformed request")
		return
	}

	if len(payload.Body) > 140 {
		handleError(w, http.StatusBadRequest, "Chirp too long")
		return
	}

	cleanBody := filterBannedWords(payload.Body)

	timestamp := time.Now()

	dbRes, err := cfg.db.CreateChirp(context.Background(), database.CreateChirpParams{
		ID:        uuid.New(),
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
		Body:      cleanBody,
		UserID:    payload.UserId,
	})
	if err != nil {
		handleError(w, http.StatusInternalServerError, "could not create chirp")
		return
	}

	w.WriteHeader(http.StatusCreated)
	resp, _ := json.Marshal(
		chirp{Id: dbRes.ID, CreatedAt: dbRes.CreatedAt, UpdatedAt: dbRes.UpdatedAt, Body: dbRes.Body, UserId: dbRes.UserID})
	w.Write(resp)
}

func (cfg *apiConfig) handleGetChirp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("chirpId")
	parsedId, err := uuid.Parse(id)
	if err != nil {
		handleError(w, http.StatusBadRequest, "invalid chirp id")
		return
	}

	dbRes, err := cfg.db.GetChirp(context.Background(), parsedId)
	if err != nil {
		handleError(w, http.StatusNotFound, "could not get chirp")
		return
	}

	resp, _ := json.Marshal(
		chirp{Id: dbRes.ID, CreatedAt: dbRes.CreatedAt, UpdatedAt: dbRes.UpdatedAt, Body: dbRes.Body, UserId: dbRes.UserID})
	w.Write(resp)
}

func (cfg *apiConfig) handleGetAllChirps(w http.ResponseWriter, r *http.Request) {
	dbChirps, err := cfg.db.GetAllChirps(context.Background())
	if err != nil {
		handleError(w, http.StatusInternalServerError, "couldn't get all chirps")
		return
	}
	chirps := make([]chirp, len(dbChirps))

	for i, c := range dbChirps {
		chirps[i] = chirp{
			Id:        c.ID,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			Body:      c.Body,
			UserId:    c.UserID,
		}
	}

	resp, _ := json.Marshal(chirps)
	w.Write(resp)
}

func handleError(w http.ResponseWriter, statusCode int, err string) {
	w.WriteHeader(statusCode)
	resp, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: err})
	w.Write(resp)
}

func checkEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("PLATFORM") != "dev" {
			handleError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	err := cfg.db.ClearUsers(context.Background())
	if err != nil {
		fmt.Printf("error clearing %v\n", err)
	}
	cfg.fileserverHits.Store(0)
	fmt.Fprintf(w, "Hits reset\n")
}

type createUserPayload struct {
	Email string `json:"email"`
}

func (cfg *apiConfig) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	payload := createUserPayload{}
	err := decoder.Decode(&payload)
	if err != nil {
		handleError(w, http.StatusBadRequest, "malformed request")
		return
	}

	timestamp := time.Now()

	db, err := cfg.db.CreateUser(context.Background(), database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
		Email:     payload.Email,
	})
	if err != nil {
		if pqError, ok := errors.AsType[*pq.Error](err); ok {
			if pqError.Code == "23505" {
				handleError(w, http.StatusConflict, "user already exists")
				return
			}
		}
		handleError(w, http.StatusInternalServerError, fmt.Sprintf("couldn't create user %v", err))
		return
	}

	resp, _ := json.Marshal(struct {
		Id        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
	}{
		Id:        db.ID,
		CreatedAt: db.CreatedAt,
		UpdatedAt: db.UpdatedAt,
		Email:     db.Email,
	})
	w.WriteHeader(http.StatusCreated)
	w.Write(resp)
}

func filterBannedWords(s string) string {

	var BANNED_WORDS = [...]string{
		"kerfuffle",
		"sharbert",
		"fornax",
	}
	cleaned := []string{}

	for word := range strings.SplitSeq(s, " ") {
		cleaned_word := word

		for _, banned_word := range BANNED_WORDS {
			if strings.ToLower(word) == banned_word {
				cleaned_word = "****"
			}
		}
		cleaned = append(cleaned, cleaned_word)
	}

	return strings.Join(cleaned, " ")
}
