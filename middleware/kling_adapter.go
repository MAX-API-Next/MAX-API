package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"

	"github.com/gin-gonic/gin"
)

func KlingRequestConvert() func(c *gin.Context) {
	return func(c *gin.Context) {
		originalPath := c.Request.URL.Path
		c.Set("kling_official_route", true)

		if c.Request.Method == http.MethodGet {
			taskId := c.Param("task_id")
			if taskId != "" {
				c.Request.URL.Path = "/v1/video/generations/" + taskId
				c.Set("task_id", taskId)
				c.Set("relay_mode", relayconstant.RelayModeVideoFetchByID)
			}
			c.Next()
			return
		}

		var originalReq map[string]interface{}
		if err := common.UnmarshalBodyReusable(c, &originalReq); err != nil {
			c.Next()
			return
		}

		// Support both model_name and model fields
		model, _ := originalReq["model_name"].(string)
		if model == "" {
			model, _ = originalReq["model"].(string)
		}
		prompt, _ := originalReq["prompt"].(string)

		unifiedReq := map[string]interface{}{
			"model":    model,
			"prompt":   prompt,
			"metadata": originalReq,
		}
		if image, ok := originalReq["image"]; ok {
			unifiedReq["image"] = image
		}
		if mode, ok := originalReq["mode"]; ok {
			unifiedReq["mode"] = mode
		}
		if duration, ok := originalReq["duration"]; ok {
			unifiedReq["duration"] = duration
		}

		jsonData, err := common.Marshal(unifiedReq)
		if err != nil {
			c.Next()
			return
		}

		// Rewrite request body and path
		common.CleanupBodyStorage(c)
		storage, err := common.CreateBodyStorage(jsonData)
		if err == nil {
			c.Set(common.KeyBodyStorage, storage)
			c.Request.Body = io.NopCloser(storage)
		} else {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(jsonData))
		}
		c.Request.URL.Path = "/v1/video/generations"
		switch {
		case strings.Contains(originalPath, "/omni-video"):
			c.Set("action", constant.TaskActionKlingOmniVideo)
		case strings.Contains(originalPath, "/text2video"):
			c.Set("action", constant.TaskActionTextGenerate)
		case strings.Contains(originalPath, "/image2video"):
			c.Set("action", constant.TaskActionGenerate)
		default:
			if image, ok := originalReq["image"]; !ok || image == "" {
				c.Set("action", constant.TaskActionTextGenerate)
			}
		}

		// We have to reset the request body for the next handlers
		c.Set(common.KeyRequestBody, jsonData)
		c.Next()
	}
}
