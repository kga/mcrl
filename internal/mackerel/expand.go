package mackerel

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ExpandedKey holds information mcrl adds from other endpoints (--expand),
// kept apart from the API's own fields.
const ExpandedKey = "_expanded"

// WithExpanded appends "_expanded": extra to a JSON object without
// re-encoding it, so the API's key order and number formatting are kept.
func WithExpanded(obj json.RawMessage, extra any) (json.RawMessage, error) {
	b, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(obj)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, errors.New("not a JSON object")
	}
	var out bytes.Buffer
	body := bytes.TrimSpace(trimmed[1 : len(trimmed)-1])
	out.WriteByte('{')
	out.Write(body)
	if len(body) > 0 {
		out.WriteByte(',')
	}
	out.WriteString(`"` + ExpandedKey + `":`)
	out.Write(b)
	out.WriteByte('}')
	return out.Bytes(), nil
}
