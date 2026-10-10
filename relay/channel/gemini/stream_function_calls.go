package gemini

import (
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

type geminiCallPosition struct {
	candidate int64
	part      int
}
type geminiCallIdentity struct {
	candidate int64
	id        string
}

// Gemini args are complete JSON snapshots, unlike Chat arguments deltas.
// Buffer willContinue sequences and emit each stable call ID only once.
type geminiStreamFunctionCalls struct {
	pending   map[geminiCallPosition]dto.FunctionCall
	completed map[geminiCallIdentity]dto.FunctionCall
}

func newGeminiStreamFunctionCalls() *geminiStreamFunctionCalls {
	return &geminiStreamFunctionCalls{pending: make(map[geminiCallPosition]dto.FunctionCall), completed: make(map[geminiCallIdentity]dto.FunctionCall)}
}

func (s *geminiStreamFunctionCalls) prepare(response *dto.GeminiChatResponse) error {
	for candidateIndex := range response.Candidates {
		candidate := &response.Candidates[candidateIndex]
		parts := make([]dto.GeminiPart, 0, len(candidate.Content.Parts))
		for partIndex, part := range candidate.Content.Parts {
			if part.FunctionCall == nil {
				parts = append(parts, part)
				continue
			}
			position := geminiCallPosition{candidate.Index, partIndex}
			call := *part.FunctionCall
			call.ID = strings.TrimSpace(call.ID)
			call.FunctionName = strings.TrimSpace(call.FunctionName)
			if previous, exists := s.pending[position]; exists {
				if call.ID != "" && previous.ID != "" && call.ID != previous.ID {
					return fmt.Errorf("Gemini function call changed id while incomplete")
				}
				if call.FunctionName != "" && previous.FunctionName != "" && call.FunctionName != previous.FunctionName {
					return fmt.Errorf("Gemini function call changed name while incomplete")
				}
				if call.ID == "" {
					call.ID = previous.ID
				}
				if call.FunctionName == "" {
					call.FunctionName = previous.FunctionName
				}
				if call.Arguments == nil {
					call.Arguments = previous.Arguments
				}
			}
			identity := geminiCallIdentity{candidate.Index, call.ID}
			if previous, exists := s.completed[identity]; call.ID != "" && exists {
				if call.FunctionName != "" && call.FunctionName != previous.FunctionName {
					return fmt.Errorf("Gemini completed function call changed name")
				}
				if call.Arguments != nil {
					oldArgs, err := common.Marshal(previous.Arguments)
					if err != nil {
						return err
					}
					newArgs, err := common.Marshal(call.Arguments)
					if err != nil {
						return err
					}
					if string(oldArgs) != string(newArgs) {
						return fmt.Errorf("Gemini completed function call changed arguments")
					}
				}
				continue
			}
			if call.WillContinue != nil && *call.WillContinue {
				s.pending[position] = call
				continue
			}
			if call.FunctionName == "" {
				return fmt.Errorf("Gemini completed function call is missing name")
			}
			delete(s.pending, position)
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			if call.ID != "" {
				s.completed[identity] = call
			}
			part.FunctionCall = &call
			parts = append(parts, part)
		}
		candidate.Content.Parts = parts
		if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
			for position := range s.pending {
				if position.candidate == candidate.Index {
					return fmt.Errorf("Gemini finished with an incomplete function call")
				}
			}
		}
	}
	return nil
}
