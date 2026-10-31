// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Wire/API reference: New API 4193734a/controller/model_pricing_config.go (AGPL-3.0).
package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

func pricingAPIError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, model.ErrModelPricingConflict) || errors.Is(err, model.ErrModelPricingLegacyWrite) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
}

// Bounded whole-document decode rejects trailing JSON and unknown/null fields.
func pricingRequestFields(c *gin.Context, allowed map[string]string) ([]byte, map[string]json.RawMessage, error) {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return nil, nil, errors.New("pricing request exceeds limit or cannot be read")
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, nil, errors.New("pricing request must be one JSON object")
	}
	for name, raw := range fields {
		kind, ok := allowed[name]
		if !ok || common.GetJsonType(raw) != kind {
			return nil, nil, errors.New("unknown or invalid pricing request field: " + name)
		}
	}
	return data, fields, nil
}
func GetModelPricingConfig(c *gin.Context) {
	snapshot, err := model.GetModelPricingSnapshot(c.QueryArray("model"))
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}
func UpdateModelPricingConfig(c *gin.Context) {
	_, fields, err := pricingRequestFields(c, map[string]string{"changes": "array"})
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	var raw []json.RawMessage
	if err := common.Unmarshal(fields["changes"], &raw); err != nil {
		pricingAPIError(c, errors.New("changes array required"))
		return
	}
	changes := make([]types.ModelPricingChange, 0, len(raw))
	for _, item := range raw {
		change, err := taskpricing.DecodePricingChange(item)
		if err != nil {
			pricingAPIError(c, err)
			return
		}
		changes = append(changes, change)
	}
	result, err := model.UpdateModelPricing(changes)
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	names := make([]string, 0, len(changes))
	for _, change := range changes {
		names = append(names, change.ModelName)
	}
	recordManageAudit(c, "model.pricing.update", map[string]any{"models": names, "cache_published": result.CachePublished})
	common.ApiSuccess(c, result)
}
func PreviewModelPricing(c *gin.Context) {
	data, fields, err := pricingRequestFields(c, map[string]string{"model_name": "string", "pricing": "object"})
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	if fields["model_name"] == nil || fields["pricing"] == nil {
		pricingAPIError(c, errors.New("model_name and pricing required"))
		return
	}
	var request struct {
		ModelName string                   `json:"model_name"`
		Pricing   types.ModelPricingValues `json:"pricing"`
	}
	if err := common.Unmarshal(data, &request); err != nil {
		pricingAPIError(c, err)
		return
	}
	entry, err := model.PreviewModelPricing(request.ModelName, request.Pricing)
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, entry)
}
func QuoteModelPricing(c *gin.Context) {
	data, _, err := pricingRequestFields(c, map[string]string{"model_name": "string", "context_id": "string", "expected_version": "string", "expected_contract_digest": "string", "expected_binding_version": "string", "pricing": "object", "submit": "object", "actual": "object", "group": "string", "pricing_at": "string", "request_body": "object", "request_headers": "object"})
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	var request model.ModelPricingQuoteRequest
	if err := common.Unmarshal(data, &request); err != nil {
		pricingAPIError(c, err)
		return
	}
	result, err := model.QuoteModelPricing(request)
	if err != nil {
		pricingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
