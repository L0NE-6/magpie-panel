package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/settings"
)

func TestM365CertificateCoversOnlyLoopbackAndKeepsNoCAKey(t *testing.T) {
	dir := t.TempDir()
	files := m365CertificateFiles{
		caCert:     filepath.Join(dir, "ca.pem"),
		serverCert: filepath.Join(dir, "server.pem"),
		serverKey:  filepath.Join(dir, "server-key.pem"),
	}
	if err := ensureM365Certificate(files); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(files.serverCert, files.serverKey)
	if err != nil {
		t.Fatal(err)
	}
	server, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(files.caCert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caPEM)
	if block == nil {
		t.Fatal("CA PEM was not decoded")
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !m365CAConstrained(ca) {
		t.Fatalf("CA constraints are not loopback-only: %+v", ca)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if _, err := server.Verify(x509.VerifyOptions{DNSName: host, Roots: roots}); err != nil {
			t.Errorf("verify %s: %v", host, err)
		}
	}
	if _, err := server.Verify(x509.VerifyOptions{DNSName: "192.168.1.2", Roots: roots}); err == nil {
		t.Fatal("certificate covered a non-loopback address")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(files.serverKey)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("server key is readable by others: %v", info.Mode().Perm())
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*ca*key*")); len(matches) != 0 {
		t.Fatalf("CA private key was retained: %v", matches)
	}
}

func TestM365TrustManagementIsMacOSOnly(t *testing.T) {
	dir := t.TempDir()
	files := m365CertificateFiles{
		caCert:     filepath.Join(dir, "ca.pem"),
		serverCert: filepath.Join(dir, "server.pem"),
		serverKey:  filepath.Join(dir, "server-key.pem"),
	}
	if err := ensureM365Certificate(files); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := m365CertificateFingerprint(files.caCert)
	if err != nil {
		t.Fatal(err)
	}
	old := macKeychain
	macKeychain = func(context.Context) (string, error) { return "/tmp/login.keychain-db", nil }
	t.Cleanup(func() { macKeychain = old })

	name, args, err := m365TrustCommand(context.Background(), "darwin", false, files.caCert)
	want := []string{"delete-certificate", "-t", "-Z", fingerprint, "/tmp/login.keychain-db"}
	if err != nil || name != "/usr/bin/security" || !slices.Equal(args, want) {
		t.Fatalf("macOS removal command = %q %q, %v; want %q %q", name, args, err, "/usr/bin/security", want)
	}
	if _, _, err := m365TrustCommand(context.Background(), "windows", true, files.caCert); err == nil || !strings.Contains(err.Error(), "only on macOS") {
		t.Fatalf("Windows automatic trust = %v, want macOS-only error", err)
	}
}

func TestM365HTTPSListenerAndRouteBoundary(t *testing.T) {
	fresh(t)
	old := m365ListenAddr
	m365ListenAddr = "127.0.0.1:0"
	t.Cleanup(func() { m365ListenAddr = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewM365Server()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.URL.Path) })
	if err := m.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = m.Stop(stopCtx)
	})
	m.mu.Lock()
	addr := m.ln.Addr().String()
	m.mu.Unlock()
	caPEM, err := os.ReadFile(m365CertificatePaths().caCert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("CA PEM was not added")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	}}}
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/v1/models", http.StatusOK},
		{http.MethodPost, "/v1/messages", http.StatusOK},
		{http.MethodGet, "/v1/magpie/quotas", http.StatusNotFound},
		{http.MethodGet, "/", http.StatusNotFound},
	} {
		req, err := http.NewRequest(tc.method, "https://"+addr+tc.path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, res.StatusCode, tc.want)
		}
	}
}

func TestM365PortInUseIsKeptInStatus(t *testing.T) {
	fresh(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	old := m365ListenAddr
	m365ListenAddr = ln.Addr().String()
	t.Cleanup(func() { m365ListenAddr = old })
	m := NewM365Server()
	if err := m.Start(context.Background(), http.NotFoundHandler()); err == nil {
		t.Fatal("start succeeded on an occupied port")
	}
	status := m.Status()
	if status.Running || !strings.Contains(status.Error, m365ListenAddr) || status.Error == "" {
		t.Fatalf("status did not explain the occupied port: %+v", status)
	}
}

func TestM365PreflightUsesCredentialedCORSAndPNA(t *testing.T) {
	fresh(t)
	s := settings.Load()
	s.CORSOrigins = []string{M365Origin}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	h := m365Only(New().Handler())
	r := httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
	r.Header.Set("Origin", M365Origin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "x-api-key, anthropic-version, content-type")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	exposed := strings.Join(w.Header().Values("Access-Control-Expose-Headers"), ",")
	if w.Code != http.StatusNoContent ||
		w.Header().Get("Access-Control-Allow-Origin") != M365Origin ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Private-Network") != "true" ||
		!strings.Contains(exposed, "anthropic-ratelimit-requests-limit") {
		t.Fatalf("preflight: %d %#v %s", w.Code, w.Header(), w.Body.String())
	}
}
