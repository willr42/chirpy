package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/willr42/chirpy/internal/authentication"
	"github.com/willr42/chirpy/internal/database"
)

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
