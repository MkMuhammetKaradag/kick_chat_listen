package models

import "encoding/json"

// Message struct
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}
