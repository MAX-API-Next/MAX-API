package openaicompat

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestResponsesToolNamesRejectAmbiguousCustomConversion(t *testing.T) {
	for _, tools := range []string{
		`[{"type":"function","name":"lookup"},{"type":"custom","name":"lookup"}]`,
		`[{"type":"custom","name":"lookup"},{"type":"function","name":"lookup"}]`,
		`[{"type":"custom","name":"lookup"},{"type":"custom","name":"lookup"}]`,
	} {
		_, err := responsesRequestToolsToChat([]byte(tools))
		require.Error(t, err, tools)
		require.Contains(t, err.Error(), "lookup")
	}
	tools, err := responsesRequestToolsToChat([]byte(`[{"type":"function","name":"lookup"},{"type":"custom","name":"patch"}]`))
	require.NoError(t, err)
	require.Len(t, tools, 2)
}

func TestResponsesCustomToolPreservesGrammarConstraint(t *testing.T) {
	for _, syntax := range []string{"lark", "regex"} {
		for _, description := range []string{"", "Apply a patch"} {
			definition := "start: \"你好\"\n  | /[a-z]+/"
			raw, err := common.Marshal([]map[string]any{{"type": "custom", "name": "patch", "description": description,
				"format": map[string]any{"type": "grammar", "syntax": syntax, "definition": definition}}})
			require.NoError(t, err)
			tools, err := responsesRequestToolsToChat(raw)
			require.NoError(t, err)
			require.Len(t, tools, 1)
			require.Contains(t, tools[0].Function.Description, definition)
			require.Contains(t, tools[0].Function.Description, description)
		}
	}
}
