package httpjson_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/azrtydxb/novaedge/internal/pkg/httpjson"
)

const contentTypeJSON = "application/json"

// unmarshalableValue is a type that cannot be JSON-encoded because it contains
// a channel, which json.Marshal does not support.
type unmarshalableValue struct {
	Ch chan int
}

func TestWriteJSON_Success(t *testing.T) {
	payload := map[string]string{"key": "value"}
	rec := httptest.NewRecorder()

	httpjson.WriteJSON(rec, http.StatusOK, payload)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("expected key=value, got key=%q", got["key"])
	}
}

func TestWriteJSON_EncodingFailureReturns500(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.WriteJSON(rec, http.StatusCreated, unmarshalableValue{Ch: make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d on encode failure, got %d", http.StatusInternalServerError, rec.Code)
	}
	// Must not have already committed the originally-requested status code or
	// Content-Type header, which would indicate double-WriteHeader.
	if ct := rec.Header().Get("Content-Type"); ct == contentTypeJSON {
		t.Error("Content-Type should not be application/json when encoding failed")
	}
}

func TestWriteJSONCompact_Success(t *testing.T) {
	payload := map[string]int{"count": 42}
	rec := httptest.NewRecorder()

	httpjson.WriteJSONCompact(rec, http.StatusAccepted, payload)

	if rec.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	var got map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if got["count"] != 42 {
		t.Errorf("expected count=42, got count=%d", got["count"])
	}
}

func TestWriteJSONCompact_EncodingFailureReturns500(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.WriteJSONCompact(rec, http.StatusCreated, unmarshalableValue{Ch: make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d on encode failure, got %d", http.StatusInternalServerError, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == contentTypeJSON {
		t.Error("Content-Type should not be application/json when encoding failed")
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()

	httpjson.WriteError(rec, http.StatusBadRequest, "something went wrong")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if got["error"] != "something went wrong" {
		t.Errorf("expected error message %q, got %q", "something went wrong", got["error"])
	}
}
