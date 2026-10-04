package agent

import "encoding/json"

func jsonUnmarshalProbe(payload string, v any) error { return json.Unmarshal([]byte(payload), v) }
