package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
	"golang.org/x/crypto/bcrypt"
)

const (
	challengeTTL         = 10 * time.Minute
	challengeMaxAttempts = 5
	challengeHourlyLimit = 5
	passwordMinLength    = 10
	legacyPasswordLength = 9
)

type CodeSender interface {
	SendVerificationCode(ctx context.Context, email, purpose, code string) error
}

type Service struct {
	pool             *pgxpool.Pool
	queries          *dbgen.Queries
	tokens           *TokenManager
	codeSender       CodeSender
	challengeSecret  []byte
	exposeDebugCodes bool
}

func NewService(pool *pgxpool.Pool, tokens *TokenManager, sender CodeSender, challengeSecret string, exposeDebugCodes bool) *Service {
	return &Service{
		pool:             pool,
		queries:          dbgen.New(pool),
		tokens:           tokens,
		codeSender:       sender,
		challengeSecret:  []byte(challengeSecret),
		exposeDebugCodes: exposeDebugCodes,
	}
}

func (s *Service) RequestSignupCode(ctx context.Context, input RequestCodeInput) (VerificationChallenge, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return VerificationChallenge{}, err
	}
	exists, err := s.queries.EmailExists(ctx, email)
	if err != nil {
		return VerificationChallenge{}, httpx.Internal(fmt.Errorf("check email: %w", err))
	}
	if exists {
		return VerificationChallenge{}, httpx.Conflict("email_already_registered", "이미 가입된 이메일입니다")
	}
	return s.requestCode(ctx, email, dbgen.ChallengePurposeSIGNUP)
}

func (s *Service) VerifySignupCode(ctx context.Context, input VerifyCodeInput) (VerificationResult, error) {
	return s.verifyCode(ctx, input, dbgen.ChallengePurposeSIGNUP)
}

// RequestResetCode intentionally returns the same shape whether the account exists or not.
// This prevents the endpoint from becoming an account-enumeration oracle.
func (s *Service) RequestResetCode(ctx context.Context, input RequestCodeInput) (VerificationChallenge, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return VerificationChallenge{}, err
	}
	exists, err := s.queries.EmailExists(ctx, email)
	if err != nil {
		return VerificationChallenge{}, httpx.Internal(fmt.Errorf("check email: %w", err))
	}
	if !exists {
		return VerificationChallenge{ExpiresAt: time.Now().UTC().Add(challengeTTL)}, nil
	}
	return s.requestCode(ctx, email, dbgen.ChallengePurposePASSWORDRESET)
}

func (s *Service) VerifyResetCode(ctx context.Context, input VerifyCodeInput) (VerificationResult, error) {
	return s.verifyCode(ctx, input, dbgen.ChallengePurposePASSWORDRESET)
}

func (s *Service) ResetPassword(ctx context.Context, input CompleteChallengeInput) error {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if err := validatePassword(input.Password, input.VerificationToken == ""); err != nil {
		return err
	}
	challengeID, err := s.resolveVerifiedChallenge(ctx, input.VerificationToken, email, dbgen.ChallengePurposePASSWORDRESET)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return httpx.Internal(fmt.Errorf("hash password: %w", err))
	}
	account, err := s.queries.GetAccountByEmail(ctx, email)
	if err != nil {
		return httpx.Unauthorized("invalid_verification_token", "비밀번호 재설정 인증이 올바르지 않습니다")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if affected, err := queries.ConsumeChallenge(ctx, challengeID); err != nil || affected != 1 {
		return httpx.Conflict("verification_already_used", "이미 사용했거나 만료된 인증입니다")
	}
	if err := queries.UpdatePassword(ctx, dbgen.UpdatePasswordParams{ID: account.ID, PasswordHash: pgutil.Text(string(hash))}); err != nil {
		return httpx.Internal(fmt.Errorf("update password: %w", err))
	}
	if err := queries.RevokeAllRefreshTokens(ctx, account.ID); err != nil {
		return httpx.Internal(fmt.Errorf("revoke sessions: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

func (s *Service) RequestWithdrawCode(ctx context.Context, accountID uuid.UUID) (VerificationChallenge, error) {
	account, err := s.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		return VerificationChallenge{}, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	return s.requestCode(ctx, account.Email, dbgen.ChallengePurposeWITHDRAW)
}

func (s *Service) VerifyWithdrawCode(ctx context.Context, accountID uuid.UUID, codeInput VerifyCodeInput) (VerificationResult, error) {
	account, err := s.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		return VerificationResult{}, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	codeInput.Email = account.Email
	return s.verifyCode(ctx, codeInput, dbgen.ChallengePurposeWITHDRAW)
}

func (s *Service) Withdraw(ctx context.Context, accountID uuid.UUID, verificationToken string) error {
	account, err := s.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		return httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	challengeID, err := s.resolveVerifiedChallenge(ctx, verificationToken, account.Email, dbgen.ChallengePurposeWITHDRAW)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if affected, err := queries.ConsumeChallenge(ctx, challengeID); err != nil || affected != 1 {
		return httpx.Conflict("verification_already_used", "이미 사용했거나 만료된 인증입니다")
	}
	if affected, err := queries.SoftDeleteAccount(ctx, accountID); err != nil {
		return httpx.Internal(fmt.Errorf("withdraw account: %w", err))
	} else if affected != 1 {
		return httpx.Conflict("account_already_withdrawn", "이미 탈퇴했거나 존재하지 않는 계정입니다")
	}
	if err := queries.SoftDeleteAccountDocuments(ctx, accountID); err != nil {
		return httpx.Internal(fmt.Errorf("withdraw account documents: %w", err))
	}
	if err := queries.ScheduleAccountArtifactCleanup(ctx, accountID); err != nil {
		return httpx.Internal(fmt.Errorf("schedule account artifact cleanup: %w", err))
	}
	if err := queries.DeleteChallengesByEmail(ctx, account.Email); err != nil {
		return httpx.Internal(fmt.Errorf("delete account challenges: %w", err))
	}
	if err := queries.RevokeAllRefreshTokens(ctx, accountID); err != nil {
		return httpx.Internal(fmt.Errorf("revoke sessions: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

func (s *Service) Signup(ctx context.Context, input SignupInput) (TokenPair, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return TokenPair{}, err
	}
	if err := validatePassword(input.Password, input.VerificationToken == ""); err != nil {
		return TokenPair{}, err
	}
	challengeID, err := s.resolveVerifiedChallenge(ctx, input.VerificationToken, email, dbgen.ChallengePurposeSIGNUP)
	if err != nil {
		return TokenPair{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return TokenPair{}, httpx.Internal(fmt.Errorf("hash password: %w", err))
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TokenPair{}, httpx.Internal(fmt.Errorf("begin signup transaction: %w", err))
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if affected, err := queries.ConsumeChallenge(ctx, challengeID); err != nil || affected != 1 {
		return TokenPair{}, httpx.Conflict("verification_already_used", "이미 사용했거나 만료된 이메일 인증입니다")
	}
	account, err := queries.CreateAccount(ctx, dbgen.CreateAccountParams{
		ID:           idgen.New(),
		Email:        email,
		PasswordHash: pgutil.Text(string(passwordHash)),
		Name:         pgutil.Text(strings.TrimSpace(input.Name)),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TokenPair{}, httpx.Conflict("email_already_registered", "이미 가입된 이메일입니다")
		}
		return TokenPair{}, httpx.Internal(fmt.Errorf("create account: %w", err))
	}
	pair, err := s.issueTokenPair(ctx, queries, account)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, httpx.Internal(fmt.Errorf("commit signup: %w", err))
	}
	return pair, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (TokenPair, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return TokenPair{}, httpx.Unauthorized("invalid_credentials", "이메일 또는 비밀번호가 올바르지 않습니다")
	}
	account, err := s.queries.GetAccountByEmail(ctx, email)
	if err != nil || !account.PasswordHash.Valid || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash.String), []byte(input.Password)) != nil {
		return TokenPair{}, httpx.Unauthorized("invalid_credentials", "이메일 또는 비밀번호가 올바르지 않습니다")
	}
	return s.issueTokenPair(ctx, s.queries, account)
}

func (s *Service) Refresh(ctx context.Context, raw string) (TokenPair, error) {
	hash := RefreshTokenHash(raw)
	stored, err := s.queries.GetRefreshToken(ctx, hash)
	if err != nil {
		return TokenPair{}, httpx.Unauthorized("invalid_refresh_token", "새로 로그인해 주세요")
	}
	account, err := s.queries.GetAccountByID(ctx, stored.AccountID)
	if err != nil {
		return TokenPair{}, httpx.Unauthorized("invalid_refresh_token", "새로 로그인해 주세요")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TokenPair{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := queries.RevokeRefreshToken(ctx, hash); err != nil {
		return TokenPair{}, httpx.Internal(err)
	}
	pair, err := s.issueTokenPair(ctx, queries, account)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, httpx.Internal(err)
	}
	return pair, nil
}

func (s *Service) Logout(ctx context.Context, rawRefreshToken string) error {
	if rawRefreshToken == "" {
		return nil
	}
	return s.queries.RevokeRefreshToken(ctx, RefreshTokenHash(rawRefreshToken))
}

func (s *Service) issueTokenPair(ctx context.Context, queries *dbgen.Queries, account dbgen.Account) (TokenPair, error) {
	access, err := s.tokens.CreateAccessToken(account.ID, compatibilityRole(account))
	if err != nil {
		return TokenPair{}, httpx.Internal(fmt.Errorf("create access token: %w", err))
	}
	refresh, refreshHash, refreshExpiresAt, err := s.tokens.NewRefreshToken()
	if err != nil {
		return TokenPair{}, httpx.Internal(err)
	}
	if err := queries.CreateRefreshToken(ctx, dbgen.CreateRefreshTokenParams{
		ID:        idgen.New(),
		AccountID: account.ID,
		TokenHash: refreshHash,
		ExpiresAt: pgutil.Time(refreshExpiresAt),
	}); err != nil {
		return TokenPair{}, httpx.Internal(fmt.Errorf("store refresh token: %w", err))
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
	}, nil
}

func compatibilityRole(account dbgen.Account) string {
	if account.Role == dbgen.AccountRoleADMIN {
		return "ADMIN"
	}
	if account.Plan == dbgen.AccountPlanPLUS {
		return "PLUS"
	}
	return "BASIC"
}

func (s *Service) requestCode(ctx context.Context, email string, purpose dbgen.ChallengePurpose) (VerificationChallenge, error) {
	count, err := s.queries.CountRecentChallenges(ctx, dbgen.CountRecentChallengesParams{Email: email, Purpose: purpose})
	if err != nil {
		return VerificationChallenge{}, httpx.Internal(fmt.Errorf("count recent challenges: %w", err))
	}
	if count >= challengeHourlyLimit {
		return VerificationChallenge{}, &httpx.Error{Status: 429, Code: "verification_rate_limited", Message: "인증 요청이 너무 많습니다. 잠시 후 다시 시도해 주세요"}
	}
	challengeID := idgen.New()
	code, err := randomCode()
	if err != nil {
		return VerificationChallenge{}, httpx.Internal(err)
	}
	expiresAt := time.Now().UTC().Add(challengeTTL)
	if _, err := s.queries.CreateVerificationChallenge(ctx, dbgen.CreateVerificationChallengeParams{
		ID:          challengeID,
		Email:       email,
		Purpose:     purpose,
		CodeHash:    s.hashCode(challengeID, code),
		MaxAttempts: challengeMaxAttempts,
		ExpiresAt:   pgutil.Time(expiresAt),
	}); err != nil {
		return VerificationChallenge{}, httpx.Internal(fmt.Errorf("create challenge: %w", err))
	}
	if err := s.codeSender.SendVerificationCode(ctx, email, string(purpose), code); err != nil {
		return VerificationChallenge{}, httpx.Internal(fmt.Errorf("send verification code: %w", err))
	}
	result := VerificationChallenge{ChallengeID: challengeID.String(), ExpiresAt: expiresAt}
	if s.exposeDebugCodes {
		result.DebugCode = code
	}
	return result, nil
}

func (s *Service) verifyCode(ctx context.Context, input VerifyCodeInput, purpose dbgen.ChallengePurpose) (VerificationResult, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil || len(input.Code) != 6 {
		return VerificationResult{}, httpx.BadRequest("invalid_verification_code", "인증 코드가 올바르지 않습니다")
	}
	challengeID, err := s.resolveOpenChallenge(ctx, input.ChallengeID, email, purpose)
	if err != nil {
		return VerificationResult{}, err
	}
	challenge, err := s.queries.IncrementChallengeAttempt(ctx, challengeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerificationResult{}, httpx.BadRequest("expired_or_locked_challenge", "인증 요청이 만료되었거나 잠겼습니다")
	}
	if err != nil {
		return VerificationResult{}, httpx.Internal(fmt.Errorf("increment challenge attempt: %w", err))
	}
	if challenge.Email != email || challenge.Purpose != purpose || !hmac.Equal([]byte(challenge.CodeHash), []byte(s.hashCode(challengeID, input.Code))) {
		return VerificationResult{}, httpx.BadRequest("invalid_verification_code", "인증 코드가 올바르지 않습니다")
	}
	if affected, err := s.queries.MarkChallengeVerified(ctx, challengeID); err != nil || affected != 1 {
		if err != nil {
			return VerificationResult{}, httpx.Internal(fmt.Errorf("verify challenge: %w", err))
		}
		return VerificationResult{}, httpx.Conflict("challenge_already_used", "이미 처리된 인증 요청입니다")
	}
	token, err := s.tokens.CreateVerificationToken(challengeID, email, string(purpose))
	if err != nil {
		return VerificationResult{}, httpx.Internal(fmt.Errorf("create verification token: %w", err))
	}
	return VerificationResult{VerificationToken: token}, nil
}

func (s *Service) resolveOpenChallenge(ctx context.Context, rawID, email string, purpose dbgen.ChallengePurpose) (uuid.UUID, error) {
	if rawID != "" {
		challengeID, err := uuid.Parse(rawID)
		if err != nil {
			return uuid.Nil, httpx.BadRequest("invalid_challenge_id", "인증 요청 정보가 올바르지 않습니다")
		}
		return challengeID, nil
	}
	challenge, err := s.queries.GetLatestOpenChallenge(ctx, dbgen.GetLatestOpenChallengeParams{Email: email, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httpx.BadRequest("expired_or_locked_challenge", "인증 요청이 만료되었거나 잠겼습니다")
	}
	if err != nil {
		return uuid.Nil, httpx.Internal(fmt.Errorf("find challenge: %w", err))
	}
	return challenge.ID, nil
}

func (s *Service) resolveVerifiedChallenge(ctx context.Context, token, email string, purpose dbgen.ChallengePurpose) (uuid.UUID, error) {
	if token != "" {
		challengeID, err := s.tokens.VerifyVerificationToken(token, email, string(purpose))
		if err != nil {
			return uuid.Nil, httpx.Unauthorized("invalid_verification_token", "이메일 인증 정보가 올바르지 않습니다")
		}
		return challengeID, nil
	}
	// Compatibility path for the 2023 frontend, which did not retain the token
	// returned by the verification endpoint. Remove after clients migrate.
	challenge, err := s.queries.GetLatestVerifiedChallenge(ctx, dbgen.GetLatestVerifiedChallengeParams{Email: email, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httpx.Unauthorized("invalid_verification_token", "이메일 인증 정보가 올바르지 않습니다")
	}
	if err != nil {
		return uuid.Nil, httpx.Internal(fmt.Errorf("find verified challenge: %w", err))
	}
	return challenge.ID, nil
}

func (s *Service) hashCode(challengeID uuid.UUID, code string) string {
	mac := hmac.New(sha256.New, s.challengeSecret)
	_, _ = mac.Write([]byte(challengeID.String()))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return "", httpx.BadRequest("invalid_email", "이메일 형식이 올바르지 않습니다")
	}
	return email, nil
}

func validatePassword(password string, legacyClient bool) error {
	minimum := passwordMinLength
	if legacyClient {
		minimum = legacyPasswordLength
	}
	hasLetter := false
	hasDigit := false
	for _, character := range password {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' {
			hasLetter = true
		}
		if character >= '0' && character <= '9' {
			hasDigit = true
		}
	}
	if len(password) < minimum || len(password) > 128 || !hasLetter || !hasDigit {
		return httpx.BadRequest("invalid_password", "비밀번호는 문자와 숫자를 포함해 10자 이상이어야 합니다")
	}
	return nil
}

func randomCode() (string, error) {
	maximum := big.NewInt(1_000_000)
	value, err := rand.Int(rand.Reader, maximum)
	if err != nil {
		return "", fmt.Errorf("generate verification code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}
