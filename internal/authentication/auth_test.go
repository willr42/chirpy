package authentication

import (
	"testing"
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
