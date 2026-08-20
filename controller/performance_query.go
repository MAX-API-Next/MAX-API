package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

type performanceNumericQuery struct {
	StartAt int64
	EndAt   int64
	Hours   int
	Limit   int
}

func parsePerformanceNumericQuery(c *gin.Context) (performanceNumericQuery, error) {
	startAt, err := parseInt64Query(c, "start")
	if err != nil {
		return performanceNumericQuery{}, err
	}
	endAt, err := parseInt64Query(c, "end")
	if err != nil {
		return performanceNumericQuery{}, err
	}
	hours, err := parseIntQuery(c, "hours")
	if err != nil {
		return performanceNumericQuery{}, err
	}
	limit, err := parseIntQuery(c, "limit")
	if err != nil {
		return performanceNumericQuery{}, err
	}
	return performanceNumericQuery{
		StartAt: startAt,
		EndAt:   endAt,
		Hours:   hours,
		Limit:   limit,
	}, nil
}

func parseIntQuery(c *gin.Context, key string) (int, error) {
	value, supplied := numericQueryValue(c, key)
	if !supplied {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, &numericQueryError{key: key}
	}
	return parsed, nil
}

func parseInt64Query(c *gin.Context, key string) (int64, error) {
	value, supplied := numericQueryValue(c, key)
	if !supplied {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, &numericQueryError{key: key}
	}
	return parsed, nil
}

type numericQueryError struct {
	key string
}

func (err *numericQueryError) Error() string {
	return err.key + " must be a valid integer"
}

func numericQueryValue(c *gin.Context, key string) (string, bool) {
	values, supplied := c.Request.URL.Query()[key]
	if !supplied {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return values[0], true
}
