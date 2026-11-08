package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/httpx"
	"github.com/kga/mcrl/internal/mackerel"
)

// request is one planned API call of an update command.
type request struct {
	Method     string
	Path       string
	Query      url.Values
	Body       any // marshaled to JSON; json.RawMessage is sent as-is
	Idempotent bool
}

func (r request) bodyBytes() ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	if raw, ok := r.Body.(json.RawMessage); ok {
		return raw, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r.Body); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// send performs the request.
func (r request) send(cmd *cobra.Command, c *mackerel.Client) ([]byte, error) {
	b, err := r.bodyBytes()
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(cmd.Context(), &httpx.Request{
		Method: r.Method, Path: r.Path, Query: r.Query, Body: b, Idempotent: r.Idempotent,
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// dryRun prints the planned requests instead of sending them. One request
// prints an object, several print an array.
func dryRun(f *cmdutil.Factory, reqs []request) error {
	base, err := f.BaseURL()
	if err != nil {
		return err
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	c := &httpx.Client{BaseURL: u}
	out := make([]dryRunRequest, len(reqs))
	for i, r := range reqs {
		b, err := r.bodyBytes()
		if err != nil {
			return err
		}
		out[i] = dryRunRequest{Method: r.Method, URL: c.URL(r.Path, r.Query).String(), Body: b}
	}
	p, err := f.Printer()
	if err != nil {
		return err
	}
	if len(out) == 1 {
		return p.Print(out[0])
	}
	return p.Print(out)
}

// errAborted is returned when the user answers no to a confirmation.
var errAborted = &cmdutil.ExitError{Code: ExitError, Err: errors.New("aborted")}

// confirm asks before a destructive operation. --yes skips it; without a
// terminal and without --yes the command refuses to run (exit code 2).
func confirm(f *cmdutil.Factory, question string) error {
	if f.Yes {
		return nil
	}
	if !f.IO.CanPrompt(f.NoInput) {
		return cmdutil.Usagef("refusing to proceed without confirmation: %s (pass --yes)", strings.TrimSuffix(question, "?"))
	}
	ok, err := f.IO.Confirm(f.NoInput, question)
	if err != nil {
		return err
	}
	if !ok {
		return errAborted
	}
	return nil
}

// expandArgs replaces a "-" argument with the non-empty lines of stdin.
func expandArgs(f *cmdutil.Factory, args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		if a != "-" {
			out = append(out, a)
			continue
		}
		sc := bufio.NewScanner(f.IO.In)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if s := strings.TrimSpace(sc.Text()); s != "" {
				out = append(out, s)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, cmdutil.Usagef("no arguments given (stdin was empty)")
	}
	return out, nil
}

// readDocument reads a JSON or YAML file ("-" for stdin) and returns JSON.
func readDocument(f *cmdutil.Factory, name string) (json.RawMessage, error) {
	var b []byte
	var err error
	if name == "-" {
		b, err = io.ReadAll(f.IO.In)
	} else {
		b, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSpace(b)
	if json.Valid(b) {
		return b, nil
	}
	j, err := yaml.YAMLToJSON(b)
	if err != nil {
		return nil, &cmdutil.UsageError{Err: fmt.Errorf("%s is neither valid JSON nor YAML: %w", name, err)}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, j); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// readObject reads a JSON/YAML object from a file into a map.
func readObject(f *cmdutil.Factory, name string) (map[string]any, error) {
	raw, err := readDocument(f, name)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, cmdutil.Usagef("%s must contain an object", name)
	}
	return m, nil
}

// decodeMap decodes a JSON object keeping numbers exact.
func decodeMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	return m, nil
}

// addNoRefetch registers --no-refetch, which prints the API's own answer
// instead of fetching the resource again after an update.
func addNoRefetch(cmd *cobra.Command, v *bool) {
	cmd.Flags().BoolVar(v, "no-refetch", false, `print the API response ({"success": true}) instead of fetching the updated resource`)
}

// chunk splits ids into groups of at most n.
func chunk(ids []string, n int) [][]string {
	var out [][]string
	for len(ids) > n {
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func post(path string, body any) request {
	return request{Method: http.MethodPost, Path: path, Body: body}
}

func put(path string, body any) request {
	return request{Method: http.MethodPut, Path: path, Body: body}
}

func del(path string) request {
	return request{Method: http.MethodDelete, Path: path}
}

// sendAll runs the requests (or prints them with --dry-run) and returns
// the response bodies.
func sendAll(cmd *cobra.Command, f *cmdutil.Factory, c *mackerel.Client, reqs []request) ([][]byte, bool, error) {
	if f.DryRun {
		return nil, true, dryRun(f, reqs)
	}
	out := make([][]byte, 0, len(reqs))
	for _, r := range reqs {
		b, err := r.send(cmd, c)
		if err != nil {
			return out, false, err
		}
		f.Applied = true
		out = append(out, b)
	}
	return out, false, nil
}

// printOne prints a single response body, or nothing for an empty body.
func printOne(f *cmdutil.Factory, b []byte) error {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	return printRaw(f, b)
}
