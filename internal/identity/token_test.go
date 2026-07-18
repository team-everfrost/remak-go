package identity

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTripAndLegacyAudience(t *testing.T) {
	manager := NewTokenManager("test-secret-that-is-long-enough", "remak-test", time.Minute, time.Hour)
	accountID := uuid.Must(uuid.NewV7())
	raw, err := manager.CreateAccessToken(accountID, "BASIC")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := manager.VerifyAccessToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if actor.AccountID != accountID || actor.Role != "BASIC" {
		t.Fatalf("unexpected actor: %#v", actor)
	}
	parts := strings.Split(raw, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["aud"] != accountID.String() {
		t.Fatalf("legacy client expects aud=%s, got %#v", accountID, claims["aud"])
	}
}

func TestPasswordCompatibilityWindow(t *testing.T) {
	if err := validatePassword("password1", true); err != nil {
		t.Fatalf("legacy 9-character password must remain accepted: %v", err)
	}
	if err := validatePassword("password1", false); err == nil {
		t.Fatal("new contract must require at least 10 characters")
	}
	if err := validatePassword("longpassword", false); err == nil {
		t.Fatal("password without a digit must be rejected")
	}
}

func TestRandomCodeIsSixDigits(t *testing.T) {
	for range 100 {
		code, err := randomCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 {
			t.Fatalf("code %q is not six digits", code)
		}
	}
}
