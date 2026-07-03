package ws

import "encoding/json"

type Message struct {
	Event         string          `json:"event"`
	Data          json.RawMessage `json:"data,omitempty"`
	Seq           int             `json:"seq,omitempty"`
	Timestamp     int64           `json:"timestamp,omitempty"`
	CorrelationID string          `json:"correlationId,omitempty"`
}
