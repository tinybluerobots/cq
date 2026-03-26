package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSend_Success(t *testing.T) {
	var (
		gotBody        string
		gotMethod      string
		gotContentType string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test-topic"}

	err := n.Send(context.Background(), "hello world")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "text/plain", gotContentType)
	assert.Equal(t, "hello world", gotBody)
}

func TestSend_NoTopic(t *testing.T) {
	n := New("")

	err := n.Send(context.Background(), "should not send")
	require.NoError(t, err)
	// No HTTP call made — if topic is empty, it's a no-op.
}

func TestSend_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test-topic"}

	err := n.Send(context.Background(), "fail please")
	require.Error(t, err)
}
