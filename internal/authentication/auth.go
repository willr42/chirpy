package authentication

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return hash, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}
	return match, nil
}

func MakeJWT(userID uuid.UUID, tokenSecret []byte, expiresIn time.Duration) (string, error) {
	timestamp := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.RegisteredClaims{Issuer: "chirpy-access",
			IssuedAt:  jwt.NewNumericDate(timestamp),
			ExpiresAt: jwt.NewNumericDate(timestamp.Add(expiresIn)),
			Subject:   userID.String(),
		})

	completeToken, err := token.SignedString(tokenSecret)
	if err != nil {
		return "", err
	}

	return completeToken, nil
}

func ValidateJWT(tokenString string, tokenSecret []byte) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return tokenSecret, nil
	})
	if err != nil {
		return uuid.Nil, err
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, err
	}

	return userID, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	authStr := headers.Get("Authorization")
	after, found := strings.CutPrefix(authStr, "Bearer ")
	if found == false {
		return "", errors.New("didn't find 'Bearer' in header")
	}
	trimmed := strings.TrimSpace(after)

	return trimmed, nil
}
