package dto

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateImageN(t *testing.T) {
	require.NoError(t, ValidateImageN("n", 0))
	require.NoError(t, ValidateImageN("n", MaxImageN))

	err := ValidateImageN("", -1)
	require.Error(t, err)
	require.Equal(t, fmt.Sprintf("n must be an integer between 1 and %d", MaxImageN), err.Error())

	err = ValidateImageN("parameters.n", MaxImageN+1)
	require.Error(t, err)
	require.Equal(t, fmt.Sprintf("parameters.n must be an integer between 1 and %d", MaxImageN), err.Error())
}
