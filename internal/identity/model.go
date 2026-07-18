package identity

import "time"

type Account struct {
	ID        string    `json:"uid"`
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	ImageURL  string    `json:"imageUrl,omitempty"`
	Role      string    `json:"role"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int64  `json:"expiresIn"`
}

type SignupInput struct {
	Email             string `json:"email"`
	Password          string `json:"password"`
	Name              string `json:"name,omitempty"`
	VerificationToken string `json:"verificationToken,omitempty"`
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RequestCodeInput struct {
	Email string `json:"email"`
}

type VerifyCodeInput struct {
	ChallengeID string `json:"challengeId,omitempty"`
	Email       string `json:"email"`
	Code        string `json:"code"`
}

type CompleteChallengeInput struct {
	Email             string `json:"email,omitempty"`
	Password          string `json:"password,omitempty"`
	VerificationToken string `json:"verificationToken,omitempty"`
}

type VerificationChallenge struct {
	ChallengeID string    `json:"challengeId"`
	ExpiresAt   time.Time `json:"expiresAt"`
	DebugCode   string    `json:"debugCode,omitempty"`
}

type VerificationResult struct {
	VerificationToken string `json:"verificationToken"`
}
