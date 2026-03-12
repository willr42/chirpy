package authentication

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSuccessfulHashCheck(t *testing.T) {
	pw := "test"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Errorf("could not hash %v, got %v\n", pw, err)
	}

	_, err = CheckPasswordHash(pw, hash)
	if err != nil {
		t.Errorf("hash %v did not match %v; err %v\n", hash, pw, err)
	}
}

func TestUnsuccessfulHashCheck(t *testing.T) {
	pw := "test"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Errorf("could not hash %v, got %v\n", pw, err)
	}

	match, err := CheckPasswordHash("asdf", hash)
	if err != nil {
		t.Errorf("unexpected error %v\n", err)
	}
	if match {
		t.Errorf("hash %v matched %v somehow.", hash, pw)
	}
}

func TestSuccessfulJWT(t *testing.T) {
	user_uuid := uuid.New()
	secret := make([]byte, 20)
	rand.Read(secret)
	jwt, err := MakeJWT(user_uuid, secret, time.Hour)
	if err != nil {
		t.Errorf("error making JWT: %v\n", err)
	}

	retrieved_id, err := ValidateJWT(jwt, secret)
	if err != nil {
		t.Errorf("error validating: %v\n", err)
	}

	if user_uuid != retrieved_id {
		t.Errorf("initial uuid did not match jwt id")
	}
}
func TestExpiredJWT(t *testing.T) {
	user_uuid := uuid.New()
	secret := make([]byte, 20)
	rand.Read(secret)
	token, err := MakeJWT(user_uuid, secret, -time.Second)
	if err != nil {
		t.Errorf("error making JWT: %v\n", err)
	}

	_, err = ValidateJWT(token, secret)
	if err == nil {
		t.Error("somehow validated expired token\n")
	}
}

func TestMalformedSubjectJWT(t *testing.T) {
	secret := make([]byte, 20)
	rand.Read(secret)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: "not-a-uuid",
	})
	tokenString, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("error making JWT: %v\n", err)
	}

	_, err = ValidateJWT(tokenString, secret)
	if err == nil {
		t.Error("expected error for non-UUID subject, got nil")
	}
}

func TestTamperedJWT(t *testing.T) {
	user_uuid := uuid.New()
	secret := make([]byte, 20)
	rand.Read(secret)
	tokenString, err := MakeJWT(user_uuid, secret, time.Hour)
	if err != nil {
		t.Fatalf("error making JWT: %v\n", err)
	}

	parts := strings.Split(tokenString, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("error decoding payload: %v\n", err)
	}

	tampered := strings.Replace(string(payload), user_uuid.String(), uuid.New().String(), 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(tampered))
	tamperedToken := strings.Join(parts, ".")

	_, err = ValidateJWT(tamperedToken, secret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}
