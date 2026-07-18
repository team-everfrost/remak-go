package pgutil

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func Text(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func String(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func Time(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
