package http

import (
	stdContext "context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateServerHTTP_BasePath(t *testing.T) {
	tests := []struct {
		basePath string
		path     string
		want     int
	}{
		{basePath: "/", path: "/healthz", want: http.StatusOK},
		{basePath: "/lacis", path: "/lacis/healthz", want: http.StatusOK},
		{basePath: "/lacis/", path: "/lacis/healthz", want: http.StatusOK},
		{basePath: "/lacis", path: "/healthz", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.basePath+tt.path, func(t *testing.T) {
			ctx := context.TestContext(nil)
			ctx.Config.Listen.BasePath = tt.basePath
			e := CreateServerHTTP(ctx, &Services{})

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			assert.Equal(t, tt.want, rec.Code)
		})
	}
}

// serve starts the server in the background and returns its address and a
// function stopping it and returning the error of Start.
func serve(t *testing.T, ctx *context.Context) (string, func() error) {
	t.Helper()
	stdCtx, cancel := stdContext.WithCancel(stdContext.Background())
	addrs := make(chan net.Addr, 1)
	errs := make(chan error, 1)
	go func() {
		errs <- Start(stdCtx, ctx, CreateServerHTTP(ctx, &Services{}), func(a net.Addr) { addrs <- a })
	}()
	select {
	case a := <-addrs:
		return a.String(), func() error { cancel(); return <-errs }
	case err := <-errs:
		cancel()
		t.Fatalf("server did not start: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("server did not start in time")
	}
	return "", nil
}

func TestStart_HTTP(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Listen.Address = "127.0.0.1:0"
	addr, stop := serve(t, ctx)

	resp, err := http.Get("http://" + addr + "/healthz")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"status":"ok"}`, string(body))

	assert.NoError(t, stop())
}

func TestStart_TLS(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Listen.Address = "127.0.0.1:0"
	ctx.Config.Listen.TLS.CertFile = "/etc/lacis/tls/server.crt"
	ctx.Config.Listen.TLS.KeyFile = "/etc/lacis/tls/server.key"
	cert, key := selfSigned(t)
	require.NoError(t, afero.WriteFile(ctx.Fs, ctx.Config.Listen.TLS.CertFile, cert, 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, ctx.Config.Listen.TLS.KeyFile, key, 0o600))
	addr, stop := serve(t, ctx)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(cert))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Get("https://" + addr + "/healthz")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.NoError(t, stop())
}

func TestStart_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		addr  string
		want  string
	}{
		{name: "missing certificate", addr: "127.0.0.1:0", want: "read TLS certificate"},
		{name: "missing key", addr: "127.0.0.1:0", files: map[string]string{"/c.crt": "x"}, want: "read TLS key"},
		{name: "invalid address", addr: "256.0.0.1:99999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			ctx.Config.Listen.Address = tt.addr
			if tt.want != "" {
				ctx.Config.Listen.TLS.CertFile = "/c.crt"
				ctx.Config.Listen.TLS.KeyFile = "/c.key"
			}
			for p, c := range tt.files {
				require.NoError(t, afero.WriteFile(ctx.Fs, p, []byte(c), 0o600))
			}
			err := Start(stdContext.Background(), ctx, CreateServerHTTP(ctx, &Services{}), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
