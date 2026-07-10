package model

import (
	"database/sql"
	"testing"
)

func TestIsZeroColumnDefault(t *testing.T) {
	tests := []struct {
		name  string
		value sql.NullString
		want  bool
	}{
		{name: "mysql numeric zero", value: sql.NullString{String: "0", Valid: true}, want: true},
		{name: "postgres bigint zero", value: sql.NullString{String: "0::bigint", Valid: true}, want: true},
		{name: "postgres quoted bigint zero", value: sql.NullString{String: "'0'::bigint", Valid: true}, want: true},
		{name: "missing default", value: sql.NullString{}, want: false},
		{name: "non-zero default", value: sql.NullString{String: "10", Valid: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isZeroColumnDefault(tt.value); got != tt.want {
				t.Fatalf("isZeroColumnDefault(%q) = %v, want %v", tt.value.String, got, tt.want)
			}
		})
	}
}
