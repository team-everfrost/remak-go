package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/team-everfrost/remak-go/internal/platform/httpx"
)

type accessClaims struct {
	AccountID string `json:"accountId"`
	Audience  string `json:"aud"`
	Role      string `json:"role"`
	Type      string `json:"type"`
	jwt.RegisteredClaims
}

type verificationClaims struct {
	ChallengeID string `json:"challengeId"`
	Email       string `json:"email"`
	Purpose     string `json:"purpose"`
	Type        string `json:"type"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(secret, issuer string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (m *TokenManager) CreateAccessToken(accountID uuid.UUID, role string) (string, error) {
	now := time.Now().UTC()
	claims := accessClaims{
		AccountID: accountID.String(),
		Audience:  accountID.String(),
		Role:      role,
		Type:      "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   accountID.String(),
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) VerifyAccessToken(raw string) (httpx.Actor, error) {
	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Type != "access" {
		return httpx.Actor{}, errors.New("invalid access token")
	}
	accountID, err := uuid.Parse(claims.AccountID)
	if err != nil {
		return httpx.Actor{}, errors.New("invalid account id")
	}
	return httpx.Actor{AccountID: accountID, PublicID: accountID, Role: claims.Role}, nil
}

func (m *TokenManager) CreateVerificationToken(challengeID uuid.UUID, email, purpose string) (string, error) {
	now := time.Now().UTC()
	claims := verificationClaims{
		ChallengeID: challengeID.String(),
		Email:       email,
		Purpose:     purpose,
		Type:        "verification",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   email,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) VerifyVerificationToken(raw, expectedEmail, expectedPurpose string) (uuid.UUID, error) {
	claims := &verificationClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Type != "verification" || claims.Email != expectedEmail ||
		claims.Purpose != expectedPurpose {
		return uuid.Nil, errors.New("invalid verification token")
	}
	return uuid.Parse(claims.ChallengeID)
}

func (m *TokenManager) NewRefreshToken() (plain string, hash []byte, expiresAt time.Time, err error) {
	buffer := make([]byte, 32)
	if _, err = rand.Read(buffer); err != nil {
		return "", nil, time.Time{}, fmt.Errorf("read random bytes: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(buffer)
	digest := sha256.Sum256([]byte(plain))
	return plain, digest[:], time.Now().UTC().Add(m.refreshTTL), nil
}

func RefreshTokenHash(raw string) []byte {
	digest := sha256.Sum256([]byte(raw))
	return digest[:]
}

func (m *TokenManager) AccessTTL() time.Duration { return m.accessTTL }
