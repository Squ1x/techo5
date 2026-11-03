package luna

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthFailureAndSingleClientBusyRetryWithBackoff(t *testing.T) {
	attempts := make(chan time.Time, 10)
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts <- time.Now()
		if count.Add(1) == 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, "Terminal busy", http.StatusConflict)
		}
	}))
	defer srv.Close()
	c := New(Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/terminal", Token: testToken}, Callbacks{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	a, b, d := take(t, attempts), take(t, attempts), take(t, attempts)
	if b.Sub(a) < 200*time.Millisecond || d.Sub(b) < 400*time.Millisecond {
		t.Fatal("retry loop did not back off")
	}
	if c.Ready() || c.Busy() {
		t.Fatal("refused handshake marked ready")
	}
	cancel()
	if err := take(t, done); err != nil {
		t.Fatal(err)
	}
}
