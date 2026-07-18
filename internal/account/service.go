package account

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const (
	freeStorageBytes  int64 = 1 << 30
	plusStorageBytes  int64 = 10 << 30
	adminStorageBytes int64 = 1 << 40
)

type Service struct {
	queries *dbgen.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{queries: dbgen.New(pool)}
}

func (s *Service) Get(ctx context.Context, accountID uuid.UUID) (Profile, error) {
	row, err := s.queries.GetAccountByID(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	if err != nil {
		return Profile{}, httpx.Internal(fmt.Errorf("get account: %w", err))
	}
	return profileFromRow(row), nil
}

func (s *Service) Update(ctx context.Context, accountID uuid.UUID, input UpdateProfileInput) (Profile, error) {
	name := strings.TrimSpace(input.Name)
	if len(name) > 100 {
		return Profile{}, httpx.BadRequest("invalid_name", "이름은 100자 이하여야 합니다")
	}
	row, err := s.queries.UpdateAccountProfile(ctx, dbgen.UpdateAccountProfileParams{
		ID:       accountID,
		Name:     pgutil.Text(name),
		ImageUrl: pgutil.Text(strings.TrimSpace(input.ImageURL)),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	if err != nil {
		return Profile{}, httpx.Internal(fmt.Errorf("update account: %w", err))
	}
	return profileFromRow(row), nil
}

func (s *Service) StorageUsage(ctx context.Context, accountID uuid.UUID) (int64, error) {
	usage, err := s.queries.GetStorageUsage(ctx, accountID)
	if err != nil {
		return 0, httpx.Internal(fmt.Errorf("get storage usage: %w", err))
	}
	return usage, nil
}

func (s *Service) StorageLimit(ctx context.Context, accountID uuid.UUID) (int64, error) {
	row, err := s.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		return 0, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	if row.Role == dbgen.AccountRoleADMIN {
		return adminStorageBytes, nil
	}
	if row.Plan == dbgen.AccountPlanPLUS {
		return plusStorageBytes, nil
	}
	return freeStorageBytes, nil
}

func profileFromRow(row dbgen.Account) Profile {
	return Profile{
		UID:       row.ID.String(),
		Email:     row.Email,
		Name:      pgutil.String(row.Name),
		ImageURL:  pgutil.String(row.ImageUrl),
		Role:      compatibilityRole(row),
		Plan:      string(row.Plan),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func compatibilityRole(row dbgen.Account) string {
	if row.Role == dbgen.AccountRoleADMIN {
		return "ADMIN"
	}
	if row.Plan == dbgen.AccountPlanPLUS {
		return "PLUS"
	}
	return "BASIC"
}
