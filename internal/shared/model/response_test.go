package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"battle-squad/internal/shared/observability"
)

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestWriteOKWrapsData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, []string{"a"})

	body := decode(t, rec)
	if rec.Code != http.StatusOK || body["success"] != true {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	if data, ok := body["data"].([]interface{}); !ok || len(data) != 1 {
		t.Fatalf("data = %v", body["data"])
	}
	if _, ok := body["error"]; ok {
		t.Fatal("success response has error field")
	}
}

func TestWriteOKNilOmitsData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, nil)

	body := decode(t, rec)
	if _, ok := body["data"]; ok || body["success"] != true {
		t.Fatalf("body = %v", body)
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), observability.CorrelationIDKey, "corr-1"))
	WriteError(rec, req, ErrItemOutOfStock)

	body := decode(t, rec)
	if rec.Code != http.StatusConflict || body["success"] != false {
		t.Fatalf("got %d %v", rec.Code, body)
	}
	e := body["error"].(map[string]interface{})
	if e["code"] != "SHOP_ITEM_OUT_OF_STOCK" || e["correlationId"] != "corr-1" {
		t.Fatalf("error = %v", e)
	}
	if _, ok := body["data"]; ok {
		t.Fatal("error response has data field")
	}
}

func TestWriteServiceError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	rec := httptest.NewRecorder()
	WriteServiceError(rec, req, ErrInsufficientBalance)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("AppError status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	WriteServiceError(rec, req, errors.New(`pq: relation "secret_table" does not exist`))
	body := decode(t, rec)
	e := body["error"].(map[string]interface{})
	if rec.Code != http.StatusInternalServerError || e["code"] != ErrInternalServer.Code || e["message"] != ErrInternalServer.Message {
		t.Fatalf("unexpected error leaked: %d %v", rec.Code, e)
	}
}
