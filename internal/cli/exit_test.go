package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kga/mcrl/internal/cli/cmdutil"
)

type status404 struct{}

func (status404) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Not Found"}}`)),
		Request:    r,
	}, nil
}

// On a terminal the hint line says where the API key came from, and the key
// itself is never printed.
func TestErrorHintNamesKeySourceOnTTY(t *testing.T) {
	t.Setenv("MCRL_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("MACKEREL_APIKEY", "secret-key")
	t.Setenv("MCRL_API_BASE", "https://api.example.test")
	t.Setenv("MCRL_PROFILE", "work")
	var errOut bytes.Buffer
	streams := &cmdutil.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: &errOut, ErrTTY: true}
	code := Main(context.Background(), []string{"api", "/hosts/abc"}, streams, WithTransport(status404{}))
	if code != ExitNotFound {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	out := errOut.String()
	want := `  hint: using the API key from env:MACKEREL_APIKEY (profile "work", https://api.example.test)`
	if !strings.Contains(out, want) {
		t.Errorf("stderr = %q, want it to contain %q", out, want)
	}
	if strings.Contains(out, "secret-key") {
		t.Errorf("stderr leaks the key: %q", out)
	}
}
