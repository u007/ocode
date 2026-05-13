package tool

import (
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Definition() map[string]interface{}
	Execute(args json.RawMessage) (string, error)
}
