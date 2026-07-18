package idgen

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a time-ordered UUIDv7 suitable for PostgreSQL primary keys.
func New() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("generate UUIDv7: %v", err))
	}
	return id
}
