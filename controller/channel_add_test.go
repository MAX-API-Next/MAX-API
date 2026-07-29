package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelsForInsertUsesIndependentNamesWithKeyPrefixes(t *testing.T) {
	base := &model.Channel{
		Name: "OpenAI Prod",
		Type: 1,
		Key:  "original",
	}
	keys := []string{"alpha-key-1", "bravo-key-2"}

	channels, names := buildChannelsForInsert(base, keys, true)

	require.Len(t, channels, 2)
	assert.Equal(t, "OpenAI Prod", base.Name)
	assert.Equal(t, "OpenAI Prod alpha-ke", channels[0].Name)
	assert.Equal(t, "OpenAI Prod bravo-ke", channels[1].Name)
	assert.Equal(t, "alpha-key-1", channels[0].Key)
	assert.Equal(t, "bravo-key-2", channels[1].Key)
	assert.Equal(t, []string{"OpenAI Prod alpha-ke", "OpenAI Prod bravo-ke"}, names)
}

func TestValidateChannelRejectsInvalidStatusCodeMapping(t *testing.T) {
	invalidMapping := `{"429":999}`
	channel := &model.Channel{
		Name:              "OpenAI Prod",
		Type:              1,
		Key:               "key",
		Models:            "gpt-test",
		StatusCodeMapping: &invalidMapping,
	}

	require.ErrorContains(t, validateChannel(channel, true), "status code mapping")
}
