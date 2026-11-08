// Package output writes command results. mcrl only emits JSON and JSONL, so
// that the output follows the API: indented and colored on a TTY, compact
// otherwise.
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/itchyny/gojq"
)

// Format is the output format.
type Format string

const (
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

// ParseFormat validates a --output value.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case "", FormatJSON:
		return FormatJSON, nil
	case FormatJSONL:
		return FormatJSONL, nil
	}
	return "", fmt.Errorf("invalid output format %q (valid: json, jsonl)", s)
}

// Printer writes JSON values to Out.
type Printer struct {
	Out    io.Writer
	Format Format
	// Pretty indents the JSON (TTY default; --compact turns it off).
	Pretty bool
	Color  bool
	// Query is the compiled --jq expression, or nil.
	Query *gojq.Code
}

// JQError is a run-time failure of the --jq expression (not a syntax error,
// and not a failure to write the output).
type JQError struct{ Err error }

func (e *JQError) Error() string { return "--jq: " + e.Err.Error() }
func (e *JQError) Unwrap() error { return e.Err }

// CompileJQ parses a --jq expression.
func CompileJQ(expr string) (*gojq.Code, error) {
	q, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid --jq expression: %w", err)
	}
	code, err := gojq.Compile(q, gojq.WithEnvironLoader(func() []string { return nil }))
	if err != nil {
		return nil, fmt.Errorf("invalid --jq expression: %w", err)
	}
	return code, nil
}

// PrintRaw writes a JSON document as received from the API. The number
// formatting and key order of the API response are preserved unless --jq
// is used.
func (p *Printer) PrintRaw(doc []byte) error {
	doc = bytes.TrimSpace(doc)
	if len(doc) == 0 {
		return nil
	}
	if !json.Valid(doc) {
		return errors.New("response is not valid JSON")
	}
	if p.Query != nil {
		v, err := decode(doc)
		if err != nil {
			return err
		}
		return p.runQuery(v)
	}
	if p.Format == FormatJSONL && doc[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(doc, &items); err != nil {
			return err
		}
		for _, it := range items {
			if err := p.writeLine(it, false); err != nil {
				return err
			}
		}
		return nil
	}
	return p.writeLine(doc, p.Pretty && p.Format == FormatJSON)
}

// Print marshals v and writes it like PrintRaw.
func (p *Printer) Print(v any) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	return p.PrintRaw(b)
}

func (p *Printer) runQuery(v any) error {
	iter := p.Query.Run(v)
	for {
		r, ok := iter.Next()
		if !ok {
			return nil
		}
		if err, ok := r.(error); ok {
			var he *gojq.HaltError
			if errors.As(err, &he) && he.Value() == nil {
				return nil
			}
			return &JQError{Err: err}
		}
		// 文字列は引用符なしで出す（jq -r 相当）。ID を次のコマンドに渡すため
		if s, ok := r.(string); ok {
			if _, err := io.WriteString(p.Out, s+"\n"); err != nil {
				return err
			}
			continue
		}
		items := []any{r}
		if arr, ok := r.([]any); ok && p.Format == FormatJSONL {
			items = arr // jsonl では配列の結果も 1 行 1 要素にする
		}
		for _, it := range items {
			b, err := marshal(it)
			if err != nil {
				return err
			}
			if err := p.writeLine(b, p.Pretty && p.Format == FormatJSON); err != nil {
				return err
			}
		}
	}
}

func (p *Printer) writeLine(doc []byte, pretty bool) error {
	var buf bytes.Buffer
	if pretty {
		if err := json.Indent(&buf, doc, "", "  "); err != nil {
			return err
		}
	} else {
		if err := json.Compact(&buf, doc); err != nil {
			return err
		}
	}
	out := buf.Bytes()
	if p.Color {
		out = Colorize(out)
	}
	out = append(out, '\n')
	_, err := p.Out.Write(out)
	return err
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decode parses JSON into values gojq understands, keeping integers exact.
func decode(doc []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalize(v), nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		if bi, ok := new(big.Int).SetString(x.String(), 10); ok {
			return bi
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalize(e)
		}
		return x
	}
	return v
}
