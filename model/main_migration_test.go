package model

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestTokenQuotaFieldsUseInt64(t *testing.T) {
	tokenType := reflect.TypeOf(Token{})
	for _, fieldName := range []string{"RemainQuota", "UsedQuota"} {
		field, ok := tokenType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("Token.%s is missing", fieldName)
		}
		if field.Type.Kind() != reflect.Int64 {
			t.Fatalf("Token.%s kind = %s, want int64", fieldName, field.Type.Kind())
		}
	}
}

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
