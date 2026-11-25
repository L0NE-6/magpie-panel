package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	M365Origin      = "https://pivot.claude.ai"
	M365DefaultPort = 8787
)

// m365ListenAddr is a variable so tests can use an ephemeral loopback port.
// The product endpoint stays fixed and predictable for the Office add-in.
var m365ListenAddr = fmt.Sprintf("127.0.0.1:%d", M365DefaultPort)

// M365Status describes the optional loopback-only HTTPS listener used by
// Claude for Microsoft 365. Provider routing stays on the ordinary Server.
type M365Status struct {
	Running     bool                  `json:"running"`
	URL         string                `json:"url"`
	Error       string                `json:"error,omitempty"`
	Certificate M365CertificateStatus `json:"certificate"`
}

func M365URL() string { return "https://" + m365ListenAddr }

type M365Server struct {
	mu     sync.Mutex
	server *http.Server
	ln     net.Listener
	err    string
}

func NewM365Server() *M365Server { return &M365Server{} }

func (m *M365Server) ClearError() {
	m.mu.Lock()
	m.err = ""
	m.mu.Unlock()
}

func (m *M365Server) SetError(err error) {
	m.mu.Lock()
	m.err = err.Error()
	m.mu.Unlock()
}

func (m *M365Server) failed(err error) error {
	m.SetError(err)
	return err
}

func (m *M365Server) Status() M365Status {
	m.mu.Lock()
	running, last := m.ln != nil, m.err
	m.mu.Unlock()
	return M365Status{
		Running:     running,
		URL:         M365URL(),
		Error:       last,
		Certificate: M365Certificate(),
	}
}

// Start prepares the local certificate and serves handler over TLS. It never
// installs or trusts the CA; that is a separate explicit user action.
func (m *M365Server) Start(ctx context.Context, handler http.Handler) error {
	files := m365CertificatePaths()
	if err := prepareM365Certificate(ctx, files); err != nil {
		return m.failed(err)
	}
	pair, err := tls.LoadX509KeyPair(files.serverCert, files.serverKey)
	if err != nil {
		return m.failed(fmt.Errorf("load Microsoft 365 certificate: %w", err))
	}
	ln, err := Listen(m365ListenAddr)
	if err != nil {
		return m.failed(fmt.Errorf("listen for Microsoft 365 at %s: %w", M365URL(), err))
	}
	srv := &http.Server{
		Handler:           lanGuard(m365Only(handler)),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       5 * time.Minute,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{pair},
			MinVersion:   tls.VersionTLS12,
		},
	}
	tlsLn := tls.NewListener(ln, srv.TLSConfig)
	m.mu.Lock()
	if m.ln != nil {
		m.mu.Unlock()
		_ = ln.Close()
		return m.failed(errors.New("Microsoft 365 HTTPS gateway is already running"))
	}
	m.server, m.ln, m.err = srv, tlsLn, ""
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.stop(stopCtx, srv)
	}()
	go func() {
		err := srv.Serve(tlsLn)
		m.mu.Lock()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.err = err.Error()
		}
		if m.server == srv {
			m.server, m.ln = nil, nil
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *M365Server) Stop(ctx context.Context) error {
	m.mu.Lock()
	srv := m.server
	m.mu.Unlock()
	return m.stop(ctx, srv)
}

// stop closes only the generation it was handed. A gateway restart may start
// a replacement before the old context ends.
func (m *M365Server) stop(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return nil
	}
	m.mu.Lock()
	if m.server == srv {
		m.server, m.ln = nil, nil
	}
	m.mu.Unlock()
	return srv.Shutdown(ctx)
}

// m365Only exposes only the two Anthropic routes the Office add-in needs.
// OPTIONS reaches corsGuard for the browser's preflight.
func m365Only(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == http.MethodOptions && (r.URL.Path == "/v1/models" || r.URL.Path == "/v1/messages") ||
			r.Method == http.MethodGet && r.URL.Path == "/v1/models" ||
			r.Method == http.MethodPost && r.URL.Path == "/v1/messages"
		if !allowed {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
