package relay

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
)

func TestShouldUseResponsesAudioBilling(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		usage     *dto.Usage
		want      bool
	}{
		{name: "legacy audio model", modelName: "gpt-4o-audio-preview", usage: &dto.Usage{}, want: true},
		{name: "current audio model", modelName: "gpt-audio-1.5", usage: &dto.Usage{}, want: true},
		{name: "mini audio model", modelName: "gpt-4o-mini-audio-preview", usage: &dto.Usage{}, want: true},
		{
			name:      "usage details identify mapped audio model",
			modelName: "custom-model-alias",
			usage: &dto.Usage{PromptTokensDetails: dto.InputTokenDetails{
				AudioTokens: 10,
			}},
			want: true,
		},
		{name: "text model", modelName: "gpt-5.4", usage: &dto.Usage{}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, shouldUseResponsesAudioBilling(test.modelName, test.usage))
		})
	}
}
