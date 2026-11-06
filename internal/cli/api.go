package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/httpx"
)

type apiOptions struct {
	method    string
	file      string
	fields    []string
	rawFields []string
	headers   []string
	include   bool
	paginate  bool
}

func newAPICmd(f *cmdutil.Factory) *cobra.Command {
	o := &apiOptions{}
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Send an arbitrary request to the Mackerel API",
		Long: `Send a request to the Mackerel API and print the response body as-is.

The path may omit the "/api/v0" prefix. A query string may be written in the
path or given with --field on GET and DELETE requests.

The method defaults to GET, or POST when --file is given.

--field key=value adds a parameter. On GET and DELETE it becomes a query
parameter (repeat the key for multiple values). On other methods the fields
form a JSON object body, where true, false, null and numbers are sent as
JSON values, and "@path" reads the value from a file. Use --file for
anything more complex. --raw-field always sends the value as a string.

--paginate follows "nextId" (alerts, alert logs, check monitors) and merges
the pages into one response.`,
		Example: `  # Organization of the current API key
  mcrl api /org

  # Names of all hosts
  mcrl api /hosts --jq '.hosts[].name'

  # Hosts of a service
  mcrl api /hosts -F service=web -F status=working -F status=standby

  # All alerts including closed ones
  mcrl api /alerts -F withClosed=true --paginate

  # Create a graph annotation from fields
  mcrl api -X POST /graph-annotations -F title=deploy -F service=web -F from=1762390800 -F to=1762390860

  # Create a monitor from a file
  mcrl api -X POST /monitors -f monitor.json`,
		Args: cmdutil.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPI(cmd, f, o, args[0])
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&o.method, "method", "X", "", "HTTP method (default GET, or POST with --file)")
	fl.StringVarP(&o.file, "file", "f", "", `read the JSON request body from a file ("-" for stdin)`)
	fl.StringArrayVarP(&o.fields, "field", "F", nil, "add a typed parameter as key=value (repeatable)")
	fl.StringArrayVar(&o.rawFields, "raw-field", nil, "add a string parameter as key=value, never converted to a number or boolean (repeatable)")
	fl.StringArrayVarP(&o.headers, "header", "H", nil, `add a request header as "Name: value" (repeatable)`)
	fl.BoolVarP(&o.include, "include", "i", false, "print the response status and headers to stderr")
	fl.BoolVar(&o.paginate, "paginate", false, `follow "nextId" and merge all pages (GET only)`)
	return cmd
}

func runAPI(cmd *cobra.Command, f *cmdutil.Factory, o *apiOptions, rawPath string) error {
	method := strings.ToUpper(o.method)
	if method == "" {
		method = http.MethodGet
		if o.file != "" {
			method = http.MethodPost
		}
	}
	path, query, err := splitPathQuery(rawPath)
	if err != nil {
		return &cmdutil.UsageError{Err: err}
	}
	if o.paginate && method != http.MethodGet {
		return cmdutil.Usagef("--paginate works only with GET")
	}

	req := &httpx.Request{Method: method, Path: path, Query: query, Header: http.Header{}}
	for _, h := range o.headers {
		name, value, ok := strings.Cut(h, ":")
		if !ok {
			return cmdutil.Usagef("invalid --header %q (want \"Name: value\")", h)
		}
		req.Header.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}

	fields := make([]field, 0, len(o.fields)+len(o.rawFields))
	for _, fv := range o.fields {
		fields = append(fields, field{raw: fv})
	}
	for _, fv := range o.rawFields {
		fields = append(fields, field{raw: fv, str: true})
	}

	bodyFromFields := method != http.MethodGet && method != http.MethodDelete && method != http.MethodHead
	switch {
	case o.file != "" && len(fields) > 0 && bodyFromFields:
		return cmdutil.Usagef("--file and --field cannot be combined for a %s body", method)
	case o.file != "":
		b, err := readInput(f, o.file)
		if err != nil {
			return err
		}
		if !json.Valid(b) {
			return cmdutil.Usagef("%s does not contain valid JSON", o.file)
		}
		req.Body = b
	case len(fields) > 0 && bodyFromFields:
		b, err := fieldsToJSON(fields)
		if err != nil {
			return err
		}
		req.Body = b
	case len(fields) > 0:
		for _, fv := range fields {
			k, v, ok := strings.Cut(fv.raw, "=")
			if !ok || k == "" {
				return cmdutil.Usagef("invalid --field %q (want key=value)", fv.raw)
			}
			req.Query.Add(k, v)
		}
	}

	printer, err := f.Printer()
	if err != nil {
		return err
	}

	if f.DryRun {
		return printDryRun(f, printer, req)
	}

	client, _, err := f.APIClient(cmd.Context())
	if err != nil {
		return err
	}

	if o.paginate {
		body, err := paginateNextID(cmd, client, req)
		if err != nil {
			return err
		}
		return printer.PrintRaw(body)
	}

	resp, err := client.Do(cmd.Context(), req)
	if o.include && resp != nil {
		printHeaders(f.IO.ErrOut, resp)
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(resp.Body)) == 0 {
		return nil
	}
	if !json.Valid(resp.Body) {
		_, err := f.IO.Out.Write(resp.Body)
		return err
	}
	return printer.PrintRaw(resp.Body)
}

func splitPathQuery(raw string) (string, url.Values, error) {
	path, rawQuery, _ := strings.Cut(raw, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", nil, fmt.Errorf("invalid query string in %q: %w", raw, err)
	}
	return path, q, nil
}

func readInput(f *cmdutil.Factory, name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(f.IO.In)
	}
	return os.ReadFile(name)
}

type field struct {
	raw string
	str bool // --raw-field: always a string
}

// fieldsToJSON builds a JSON object from key=value pairs with typed values.
func fieldsToJSON(fields []field) ([]byte, error) {
	obj := map[string]any{}
	for _, fv := range fields {
		k, v, ok := strings.Cut(fv.raw, "=")
		if !ok || k == "" {
			return nil, cmdutil.Usagef("invalid --field %q (want key=value)", fv.raw)
		}
		if fv.str {
			obj[k] = v
			continue
		}
		val, err := typedValue(v)
		if err != nil {
			return nil, err
		}
		obj[k] = val
	}
	return json.Marshal(obj)
}

func typedValue(v string) (any, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if strings.HasPrefix(v, "@") {
		b, err := os.ReadFile(v[1:])
		if err != nil {
			return nil, err
		}
		return strings.TrimRight(string(b), "\n"), nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	if fl, err := strconv.ParseFloat(v, 64); err == nil && !strings.ContainsAny(v, "xXnN") {
		return json.Number(strconv.FormatFloat(fl, 'f', -1, 64)), nil
	}
	return v, nil
}

func printHeaders(w io.Writer, resp *httpx.Response) {
	fmt.Fprintf(w, "HTTP %s\n", resp.Status)
	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range resp.Header[k] {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}
	}
	fmt.Fprintln(w)
}

type dryRunRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    json.RawMessage     `json:"body,omitempty"`
}

func printDryRun(f *cmdutil.Factory, p interface{ Print(any) error }, req *httpx.Request) error {
	// API キーがなくても dry-run はできる
	base, err := f.BaseURL()
	if err != nil {
		return err
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	c := &httpx.Client{BaseURL: u}
	out := dryRunRequest{Method: req.Method, URL: c.URL(req.Path, req.Query).String(), Body: req.Body}
	if len(req.Header) > 0 {
		out.Headers = req.Header
	}
	return p.Print(out)
}

// paginateNextID follows nextId and concatenates the array fields of each page.
func paginateNextID(cmd *cobra.Command, client *httpx.Client, req *httpx.Request) ([]byte, error) {
	merged := map[string]json.RawMessage{}
	var order []string
	var arrays = map[string][]json.RawMessage{}
	for page := 0; ; page++ {
		resp, err := client.Do(cmd.Context(), req)
		if err != nil {
			return nil, err
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body, &obj); err != nil {
			return nil, fmt.Errorf("--paginate needs a JSON object response: %w", err)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := obj[k]
			if k == "nextId" {
				continue
			}
			var items []json.RawMessage
			if json.Unmarshal(v, &items) == nil {
				if _, seen := arrays[k]; !seen {
					order = append(order, k)
				}
				arrays[k] = append(arrays[k], items...)
				continue
			}
			if page == 0 {
				merged[k] = v
				order = append(order, k)
			}
		}
		var next string
		if raw, ok := obj["nextId"]; ok {
			_ = json.Unmarshal(raw, &next)
		}
		if next == "" {
			break
		}
		q := url.Values{}
		for k, v := range req.Query {
			q[k] = v
		}
		q.Set("nextId", next)
		req = &httpx.Request{Method: req.Method, Path: req.Path, Query: q, Header: req.Header}
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		if items, ok := arrays[k]; ok {
			if items == nil {
				items = []json.RawMessage{}
			}
			b, err := json.Marshal(items)
			if err != nil {
				return nil, err
			}
			buf.Write(b)
		} else {
			buf.Write(merged[k])
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
