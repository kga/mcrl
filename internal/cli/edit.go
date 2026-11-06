package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/credential"
	"github.com/kga/mcrl/internal/mackerel"
)

// editable describes a resource that "edit" can change.
type editable struct {
	Kind mackerel.Kind
	// ItemPath is the PUT path with %s for the ID.
	ItemPath string
	// ReadOnly fields are removed before editing and never sent.
	ReadOnly []string
	// ResponseKey unwraps the PUT response, if needed.
	ResponseKey string
	// Validate checks the edited object before it is sent.
	Validate func(map[string]any) error
	// Finalize adjusts the body against the current object before sending.
	Finalize func(cur, body map[string]any)
}

func newEditCmd(f *cmdutil.Factory, e editable, refName string) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <" + refName + ">",
		Short: "Edit a " + e.Kind.Name + " in $EDITOR",
		Long: withAPI(fmt.Sprintf(`Open the current %s as YAML in $VISUAL or $EDITOR (default vi), then
show the changes and send them after confirmation.

The whole object is sent back, including fields mcrl does not know about,
so nothing is lost. Read-only fields (%s) are hidden. If the file does not
parse, the editor opens again with the error at the top. Saving an empty file
or leaving it unchanged cancels.`, e.Kind.Name, strings.Join(e.ReadOnly, ", ")),
			"Read, Write", "PUT /api/v0"+strings.ReplaceAll(e.ItemPath, "%s", "<id>")),
		Example:           fmt.Sprintf("  mcrl %s edit 3Ja1bAxcdef\n  EDITOR='code --wait' mcrl %s edit 3Ja1bAxcdef", cmdName(e.Kind), cmdName(e.Kind)),
		Args:              cmdutil.ExactArgs(1),
		ValidArgsFunction: completeKind(f, e.Kind),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := apiClient(cmd.Context(), f)
			if err != nil {
				return err
			}
			cur, err := c.Resolve(cmd.Context(), e.Kind, args[0])
			if err != nil {
				return err
			}
			id := mackerel.StringField(cur, "id")
			before, err := toEditableYAML(cur, e.ReadOnly)
			if err != nil {
				return err
			}
			edited, after, changed, err := editLoop(f, before, fmt.Sprintf("%s %s", e.Kind.Name, id), e.Validate)
			if err != nil || !changed {
				return err
			}
			diff := lineDiff(string(before), after)
			if diff == "" {
				fmt.Fprintln(f.IO.ErrOut, "no changes")
				return nil
			}
			fmt.Fprint(f.IO.ErrOut, colorDiff(f, diff))
			for _, k := range e.ReadOnly {
				delete(edited, k)
			}
			if e.Finalize != nil {
				curMap, err := decodeMap(cur)
				if err != nil {
					return err
				}
				e.Finalize(curMap, edited)
			}
			if !f.DryRun {
				if err := confirm(f, fmt.Sprintf("Update %s %s with these changes?", e.Kind.Name, id)); err != nil {
					return err
				}
			}
			return simpleSendUnwrap(cmd, f, put(fmt.Sprintf(e.ItemPath, url.PathEscape(id)), edited), e.ResponseKey)
		},
	}
}

func cmdName(k mackerel.Kind) string {
	return strings.ReplaceAll(k.Name, " ", "-")
}

// toEditableYAML renders an API object as YAML without the read-only fields,
// keeping the API's key order.
func toEditableYAML(raw json.RawMessage, readOnly []string) ([]byte, error) {
	stripped, err := stripFields(raw, readOnly)
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(stripped)
}

// stripFields removes top-level keys from a JSON object, keeping key order.
func stripFields(raw json.RawMessage, keys []string) (json.RawMessage, error) {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if drop[key] {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(key)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// editLoop opens the editor until the content parses (and validates) or
// the user cancels. changed is false when cancelled or unchanged.
func editLoop(f *cmdutil.Factory, content []byte, what string, validate func(map[string]any) error) (map[string]any, string, bool, error) {
	header := fmt.Sprintf("# Editing %s. Lines starting with # are ignored.\n# Save an empty file or leave it unchanged to cancel.\n", what)
	text := header + string(content)
	for {
		out, err := runEditor(f, text)
		if err != nil {
			return nil, "", false, err
		}
		body := stripComments(out)
		if strings.TrimSpace(body) == "" || body == string(content) {
			fmt.Fprintln(f.IO.ErrOut, "edit cancelled; nothing was changed")
			return nil, "", false, nil
		}
		m, perr := parseEdited(body)
		if perr == nil && validate != nil {
			perr = validate(m)
		}
		if perr == nil {
			return m, body, true, nil
		}
		if !f.IO.CanPrompt(f.NoInput) {
			return nil, "", false, &cmdutil.UsageError{Err: perr}
		}
		text = header + "# ERROR: " + strings.ReplaceAll(perr.Error(), "\n", "\n# ") + "\n" + body
	}
}

func parseEdited(body string) (map[string]any, error) {
	j, err := yaml.YAMLToJSON([]byte(body))
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	m, err := decodeMap(j)
	if err != nil || m == nil {
		return nil, errors.New("the document must be a mapping (an object)")
	}
	return m, nil
}

func stripComments(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// runEditor writes text to a private temp file, opens the editor on it and
// returns what was saved.
func runEditor(f *cmdutil.Factory, text string) (string, error) {
	tmp, err := os.CreateTemp("", "mcrl-edit-*.yaml")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := io.WriteString(tmp, text); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	editor := f.Getenv("VISUAL")
	if editor == "" {
		editor = f.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	argv, err := credential.SplitArgs(editor)
	if err != nil || len(argv) == 0 {
		return "", fmt.Errorf("invalid editor %q", editor)
	}
	cmd := exec.Command(argv[0], append(argv[1:], tmp.Name())...) //nolint:gosec // 利用者が設定したエディタ
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // エディタの出力で stdout の JSON を汚さない
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q failed: %w", editor, err)
	}
	b, err := os.ReadFile(tmp.Name())
	return string(b), err
}

// lineDiff returns a unified-style diff of two texts ("" if equal).
func lineDiff(a, b string) string {
	if a == b {
		return ""
	}
	x := strings.Split(strings.TrimRight(a, "\n"), "\n")
	y := strings.Split(strings.TrimRight(b, "\n"), "\n")
	// LCS の表（設定ファイルは小さいので O(n*m) で十分）
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	out.WriteString("--- current\n+++ edited\n")
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			out.WriteString("  " + x[i] + "\n")
			i++
			j++
		case i < len(x) && (j == len(y) || lcs[i+1][j] >= lcs[i][j+1]):
			out.WriteString("- " + x[i] + "\n") // 削除を先に出す
			i++
		default:
			out.WriteString("+ " + y[j] + "\n")
			j++
		}
	}
	return out.String()
}

func colorDiff(f *cmdutil.Factory, diff string) string {
	if !f.IO.ErrTTY || f.Getenv("NO_COLOR") != "" {
		return diff
	}
	var out strings.Builder
	for _, l := range strings.SplitAfter(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+ "):
			out.WriteString("\x1b[32m" + strings.TrimSuffix(l, "\n") + "\x1b[0m\n")
		case strings.HasPrefix(l, "- "):
			out.WriteString("\x1b[31m" + strings.TrimSuffix(l, "\n") + "\x1b[0m\n")
		default:
			out.WriteString(l)
		}
	}
	return out.String()
}
