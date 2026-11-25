//go:build windows

package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// serveWSL leaves its listeners for Shutdown. Closing them as its context
// ends makes Shutdown return net.ErrClosed and log a request as cut (#7729).
func TestServeWSLShutdownOwnsListeners(t *testing.T) {
	fresh(t)
	main, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { main.Close() })
	t.Setenv("MAGPIE_ADDR", main.Addr().String())
	if l6, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		l6.Close()
	}

	oldEverywhere, oldWatch := wslServeEverywhere, wslWatch
	wslServeEverywhere, wslWatch = true, time.Hour
	t.Cleanup(func() { wslServeEverywhere, wslWatch = oldEverywhere, oldWatch })
	fakeWSLNet(t, "::1/128")

	s := &Server{}
	s.ln = main
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.serveWSL(ctx, srv)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("serveWSL did not end")
		}
	})

	at := "http://" + net.JoinHostPort("::1", Port()) + "/"
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   time.Second,
	}
	t.Cleanup(client.CloseIdleConnections)
	waitFor(t, "the WSL listener", func() bool {
		res, err := client.Get(at)
		if err != nil {
			return false
		}
		res.Body.Close()
		return true
	})

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serveWSL did not end with its context")
	}

	res, err := client.Get(at)
	if err != nil {
		t.Fatalf("WSL listener closed before Shutdown: %v", err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "ok" {
		t.Fatalf("after serveWSL ended: %q", got)
	}

	shutCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown after serveWSL ended: %v", err)
	}
}
