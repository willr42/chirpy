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
	"github.com/willr42/chirpy/internal/authentication"
	"github.com/willr42/chirpy/internal/database"
)

type apiConfig struct {
	db             *database.Queries
	jwtSecret      []byte
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

	cfg := apiConfig{fileserverHits: atomic.Int32{}, db: dbQueries, jwtSecret: []byte(os.Getenv("JWTSECRET"))}

	mux := http.NewServeMux()
	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /admin/metrics", cfg.handleMetrics)
	mux.Handle("POST /admin/reset", checkEnv(http.HandlerFunc(cfg.handleReset)))
	mux.HandleFunc("POST /api/users", cfg.handleRegister)
	mux.HandleFunc("POST /api/login", cfg.handleLogin)
	mux.HandleFunc("POST /api/refresh", cfg.handleRefresh)
	mux.HandleFunc("POST /api/revoke", cfg.handleRevoke)
	mux.HandleFunc("GET /api/healthz", handleHealthz)
	mux.HandleFunc("GET /api/chirps", cfg.handleGetAllChirps)
	mux.HandleFunc("GET /api/chirps/{chirpId}", cfg.handleGetChirp)
	mux.Handle("POST /api/chirps", cfg.checkAuth(http.HandlerFunc(cfg.handleCreateChirp)))

	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	fmt.Println("Starting server...")
	log.Fatal(server.ListenAndServe())
}

type chirpPayload struct {
	Body string `json:"body"`
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

	userID := r.Context().Value(userIDKey).(uuid.UUID)

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
		UserID:    userID,
	})
	if err != nil {
		handleError(w, http.StatusInternalServerError, "could not create chirp")
		fmt.Printf("%v", err)
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

type contextKey string

const userIDKey contextKey = "userID"

func (cfg *apiConfig) checkAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := authentication.GetBearerToken(r.Header)
		if err != nil {
			handleError(w, http.StatusInternalServerError, "error getting headers")
			return
		}

		userId, err := authentication.ValidateJWT(token, cfg.jwtSecret)
		if err != nil || userId == uuid.Nil {
			fmt.Printf("token> %s, err> %v", token, err)
			handleError(w, http.StatusUnauthorized, "invalid auth")
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, userId)
		next.ServeHTTP(w, r.WithContext(ctx))
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

type userPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (cfg *apiConfig) handleRegister(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	payload := userPayload{}
	err := decoder.Decode(&payload)
	if err != nil || payload.Password == "" {
		handleError(w, http.StatusBadRequest, "malformed request")
		return
	}

	timestamp := time.Now()
	hashed, err := authentication.HashPassword(payload.Password)
	if err != nil {
		handleError(w, http.StatusBadRequest, "password error")
	}

	db, err := cfg.db.CreateUser(context.Background(), database.CreateUserParams{
		ID:             uuid.New(),
		CreatedAt:      timestamp,
		UpdatedAt:      timestamp,
		Email:          payload.Email,
		HashedPassword: hashed,
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

func (cfg *apiConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	payload := userPayload{}
	err := decoder.Decode(&payload)
	if err != nil {
		handleError(w, http.StatusBadRequest, "malformed request")
		return
	}

	dbUser, err := cfg.db.GetUserByEmail(context.Background(), payload.Email)
	if err != nil {
		handleError(w, http.StatusInternalServerError, fmt.Sprintf("couldn't get user %v", err))
		return
	}

	ok, err := authentication.CheckPasswordHash(payload.Password, dbUser.HashedPassword)
	if err != nil || !ok {
		handleError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	accessToken, err := authentication.MakeJWT(dbUser.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		handleError(w, http.StatusInternalServerError, "could not generate token")
	}

	refreshToken := authentication.MakeRefreshToken()
	timestamp := time.Now()

	_, err = cfg.db.CreateRefreshToken(context.Background(), database.CreateRefreshTokenParams{
		Token:     refreshToken,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
		UserID:    dbUser.ID,
		ExpiresAt: time.Now().AddDate(0, 0, 60),
	})

	resp, _ := json.Marshal(struct {
		Id           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
	}{
		Id:           dbUser.ID,
		CreatedAt:    dbUser.CreatedAt,
		UpdatedAt:    dbUser.UpdatedAt,
		Email:        dbUser.Email,
		Token:        accessToken,
		RefreshToken: refreshToken,
	})
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

func (cfg *apiConfig) handleRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := authentication.GetBearerToken(r.Header)
	if err != nil {
		fmt.Printf("err> %v", err)
		handleError(w, http.StatusUnauthorized, "couldn't get token")
		return
	}

	dbRefreshToken, err := cfg.db.GetRefreshTokenByToken(context.Background(), refreshToken)
	if err != nil {
		fmt.Printf("err> %v", err)
		handleError(w, http.StatusUnauthorized, "couldn't get token")
		return
	}

	if dbRefreshToken.RevokedAt.Valid || time.Now().After(dbRefreshToken.ExpiresAt) {
		fmt.Printf("revoked> %v", dbRefreshToken.RevokedAt)
		handleError(w, http.StatusUnauthorized, "token revoked")
		return
	}

	newAccessToken, err := authentication.MakeJWT(dbRefreshToken.UserID, cfg.jwtSecret, time.Hour)
	if err != nil {
		handleError(w, http.StatusInternalServerError, "couldn't make new token")
		return
	}

	resp, _ := json.Marshal(struct {
		Token string `json:"token"`
	}{
		Token: newAccessToken,
	})
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

func (cfg *apiConfig) handleRevoke(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := authentication.GetBearerToken(r.Header)
	if err != nil {
		fmt.Printf("err> %v", err)
		handleError(w, http.StatusUnauthorized, "couldn't get token")
		return
	}

	timestamp := time.Now()

	cfg.db.RevokeRefreshToken(context.Background(), database.RevokeRefreshTokenParams{
		RevokedAt: sql.NullTime{
			Time:  timestamp,
			Valid: true,
		},
		UpdatedAt: timestamp,
		Token:     refreshToken,
	})

	w.WriteHeader(http.StatusNoContent)
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
