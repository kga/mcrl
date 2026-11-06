package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/httpx"
	"github.com/kga/mcrl/internal/mackerel"
)

// Exit codes. See docs/04-cli-ux.md.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitNotFound    = 4
	ExitInvalid     = 5
	ExitRateLimited = 6
	ExitNetwork     = 7
	ExitInterrupted = 130
)

type classified struct {
	exit   int
	code   string
	status int
	hint   string
}

func classify(err error) classified {
	c := classified{exit: ExitError, code: "error"}

	var he *cmdutil.HintError
	if errors.As(err, &he) {
		c.hint = he.Hint
	}

	var (
		ee  *cmdutil.ExitError
		ue  *cmdutil.UsageError
		ae  *cmdutil.AuthError
		api *httpx.APIError
		ne  *httpx.NetworkError
		nf  *mackerel.NotFoundError
		amb *mackerel.AmbiguousError
	)
	switch {
	case errors.As(err, &ee):
		c.exit = ee.Code
	case errors.Is(err, context.Canceled):
		c.exit, c.code = ExitInterrupted, "interrupted"
	case errors.As(err, &ue) || isCobraUsageError(err):
		c.exit, c.code = ExitUsage, "usage"
	case errors.As(err, &ae):
		c.exit, c.code = ExitAuth, "auth"
	case errors.As(err, &nf):
		c.exit, c.code = ExitNotFound, "not_found"
	case errors.As(err, &amb):
		c.exit, c.code = ExitInvalid, "ambiguous"
		c.setHint("pass the ID, or force the interpretation with the id: or name: prefix")
	case errors.As(err, &api):
		c.status = api.StatusCode
		switch {
		case api.StatusCode == http.StatusUnauthorized:
			c.exit, c.code = ExitAuth, "unauthorized"
			c.setHint("check the API key with `mcrl auth status`")
		case api.StatusCode == http.StatusForbidden:
			c.exit, c.code = ExitAuth, "forbidden"
			c.setHint("the API key may lack Write permission, or the request comes from outside the organization's allowed IP range")
		case api.StatusCode == http.StatusNotFound:
			c.exit, c.code = ExitNotFound, "not_found"
		case api.StatusCode == http.StatusTooManyRequests:
			c.exit, c.code = ExitRateLimited, "rate_limited"
			c.setHint("wait a moment and retry")
		case api.StatusCode >= 400 && api.StatusCode < 500:
			c.exit, c.code = ExitInvalid, "invalid"
		default:
			c.exit, c.code = ExitError, "server_error"
		}
	case errors.As(err, &ne):
		c.exit, c.code = ExitNetwork, "network"
		c.setHint("check your network connection, proxy settings and the API base URL (`mcrl doctor`)")
	}
	return c
}

func (c *classified) setHint(h string) {
	if c.hint == "" {
		c.hint = h
	}
}

func isCobraUsageError(err error) bool {
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument", "required flag", "at least one of the flags", "if any flags in the group"} {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

// ExitCode maps an error to the process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	return classify(err).exit
}

// PrintError reports err on stderr: human-readable on a TTY, one JSON line otherwise.
func PrintError(io *cmdutil.IOStreams, err error) {
	var ee *cmdutil.ExitError
	if errors.As(err, &ee) && ee.Silent {
		return
	}
	c := classify(err)
	if c.exit == ExitInterrupted {
		return
	}
	msg := err.Error()
	if io.ErrTTY {
		fmt.Fprintf(io.ErrOut, "error: %s\n", msg)
		if c.hint != "" {
			fmt.Fprintf(io.ErrOut, "  hint: %s\n", c.hint)
		}
		return
	}
	type payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status,omitempty"`
		Hint    string `json:"hint,omitempty"`
	}
	b, _ := json.Marshal(struct {
		Error payload `json:"error"`
	}{payload{Code: c.code, Message: msg, Status: c.status, Hint: c.hint}})
	fmt.Fprintf(io.ErrOut, "%s\n", b)
}
