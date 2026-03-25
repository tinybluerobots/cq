package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend_Success(t *testing.T) {
	var gotBody string
	var gotMethod string
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test-topic"}
	err := n.Send("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "text/plain" {
		t.Errorf("content-type = %q, want text/plain", gotContentType)
	}
	if gotBody != "hello world" {
		t.Errorf("body = %q, want %q", gotBody, "hello world")
	}
}

func TestSend_NoTopic(t *testing.T) {
	n := New("")
	err := n.Send("should not send")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No HTTP call made — if topic is empty, it's a no-op.
}

func TestSend_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test-topic"}
	err := n.Send("fail please")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
