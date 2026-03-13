package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/willr42/chirpy/internal/authentication"
)

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

func checkEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("PLATFORM") != "dev" {
			handleError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}
