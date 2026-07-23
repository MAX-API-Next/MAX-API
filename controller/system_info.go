package controller

import (
	"errors"
	"net/http"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"

	"github.com/gin-gonic/gin"
)

func ListSystemInstances(c *gin.Context) {
	instances, err := model.ListSystemInstances()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := common.GetTimestamp()
	responses := make([]model.SystemInstanceResponse, 0, len(instances))
	for _, instance := range instances {
		responses = append(responses, instance.ToResponse(now))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responses,
	})
}

func DeleteSystemInstance(c *gin.Context) {
	err := model.DeleteStaleSystemInstance(c.Param("node_name"), common.GetTimestamp())
	if err != nil {
		switch {
		case errors.Is(err, model.ErrSystemInstanceNotFound),
			errors.Is(err, model.ErrSystemInstanceOnline):
			common.ApiErrorMsg(c, err.Error())
		default:
			common.ApiError(c, err)
		}
		return
	}

	common.ApiSuccess(c, nil)
}

func DeleteStaleSystemInstances(c *gin.Context) {
	deletedCount, err := model.DeleteStaleSystemInstances(common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"deleted_count": deletedCount,
	})
}
