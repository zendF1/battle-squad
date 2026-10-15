package model

import (
	"encoding/json"
	"net/http"
)

// Envelope is the body of every REST API response:
//
//	{"success": true,  "data": <payload>}
//	{"success": false, "error": {"code", "message", "details", "correlationId"}}
//
// Data is omitted when an endpoint has nothing to return.
type Envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

// WriteJSON writes data as a success envelope with the given status.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	writeEnvelope(w, status, Envelope{Success: true, Data: data})
}

// WriteOK writes data as a 200 success envelope; data may be nil.
func WriteOK(w http.ResponseWriter, data interface{}) {
	WriteJSON(w, http.StatusOK, data)
}

func writeEnvelope(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(env)
}
