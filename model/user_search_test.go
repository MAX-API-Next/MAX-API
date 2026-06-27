package model

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSearchUsers_FilterByQuotaStatus(t *testing.T) {
	truncateTables(t)

	users := []*User{
		{Username: "negative_balance_user", DisplayName: "Negative", Status: common.UserStatusEnabled, Quota: -100, AffCode: "neg1"},
		{Username: "zero_balance_user", DisplayName: "Zero", Status: common.UserStatusEnabled, Quota: 0, AffCode: "zero1"},
		{Username: "positive_balance_user", DisplayName: "Positive", Status: common.UserStatusEnabled, Quota: 100, AffCode: "pos1"},
	}
	require.NoError(t, DB.Create(&users).Error)

	tests := []struct {
		name        string
		quotaStatus string
		want        []string
	}{
		{
			name:        "negative",
			quotaStatus: UserQuotaStatusNegative,
			want:        []string{"negative_balance_user"},
		},
		{
			name:        "zero",
			quotaStatus: UserQuotaStatusZero,
			want:        []string{"zero_balance_user"},
		},
		{
			name:        "positive",
			quotaStatus: UserQuotaStatusPositive,
			want:        []string{"positive_balance_user"},
		},
		{
			name:        "unknown means all",
			quotaStatus: "all",
			want: []string{
				"negative_balance_user",
				"zero_balance_user",
				"positive_balance_user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total, err := SearchUsers("", "", nil, nil, tt.quotaStatus, 0, 20)
			require.NoError(t, err)
			require.EqualValues(t, len(tt.want), total)

			usernames := make([]string, 0, len(result))
			for _, user := range result {
				usernames = append(usernames, user.Username)
			}
			assert.ElementsMatch(t, tt.want, usernames)
		})
	}
}

func TestSearchUsers_EmptyKeywordDoesNotAddLikeCondition(t *testing.T) {
	query := buildSearchUsersQuery(
		DB.Session(&gorm.Session{DryRun: true}),
		"",
		"",
		nil,
		nil,
		UserQuotaStatusNegative,
	)

	stmt := query.Count(new(int64)).Statement
	sql := strings.ToUpper(stmt.SQL.String())

	assert.NotContains(t, sql, "LIKE")
	assert.Contains(t, sql, "QUOTA <")
}
