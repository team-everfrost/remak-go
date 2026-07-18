package retrieval

import (
	"testing"

	"github.com/google/uuid"
)

func TestFuseRanksRewardsAgreement(t *testing.T) {
	lexicalOnly := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	semanticOnly := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	agreed := uuid.MustParse("00000000-0000-7000-8000-000000000003")

	result := fuseRanks([]uuid.UUID{lexicalOnly, agreed}, []uuid.UUID{semanticOnly, agreed}, 3)
	if result[0] != agreed {
		t.Fatalf("agreed candidate should rank first, got %v", result)
	}
}

func TestFuseRanksCapsResults(t *testing.T) {
	first := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	second := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	result := fuseRanks([]uuid.UUID{first, second}, nil, 1)
	if len(result) != 1 || result[0] != first {
		t.Fatalf("unexpected result: %v", result)
	}
}
