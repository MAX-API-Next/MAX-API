package dto

import (
	"encoding/json"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

// AlphaSearchRequest is the Codex standalone web search request.
// RawBody preserves unknown fields for forward compatibility.
type AlphaSearchRequest struct {
	Model   string          `json:"model"`
	ID      string          `json:"id,omitempty"`
	Stream  *bool           `json:"stream,omitempty"`
	RawBody json.RawMessage `json:"-"`
}

func (r *AlphaSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	combineText := ""
	if len(r.RawBody) > 0 {
		combineText = string(r.RawBody)
	}
	return &types.TokenCountMeta{
		CombineText: combineText,
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *AlphaSearchRequest) IsStream(_ *gin.Context) bool {
	return false
}

func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
