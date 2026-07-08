package controller

import "strconv"

func sessionInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		id, err := strconv.Atoi(v)
		return id, err == nil
	default:
		return 0, false
	}
}

func sessionUserID(value any) (int, bool) {
	id, ok := sessionInt(value)
	return id, ok && id > 0
}
