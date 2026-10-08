package erkennung

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed prompt_v1.txt
var promptV1 string

//go:embed schema.json
var schemaJSON []byte

func promptText(mode string) string {
	text := strings.TrimSpace(promptV1)
	if mode == "json_object" {
		text += "\n\nJSON-Schema:\n" + strings.TrimSpace(string(schemaJSON))
	}
	return text
}

func schemaValue() (any, error) {
	var schema any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, err
	}
	return schema, nil
}
