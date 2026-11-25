package environment

import (
	"errors"
	"strings"
)

const (
	Default    = "default"
	Test       = "test"
	Production = "production"
)

func Normalize(value string) (string, error) {
	switch value = strings.TrimSpace(value); value {
	case "", Default:
		return Default, nil
	case Test, Production:
		return value, nil
	default:
		return "", errors.New("环境必须为默认、测试或线上")
	}
}
