package output

import (
	"bytes"
	"testing"
)

const hosts = `[{"id":"5Vf7Be7guuy","name":"web-1","createdAt":1793970084,"memo":"<a&b>"},{"id":"abcdefghijk","name":"web-2","createdAt":1793970085}]`

func print(t *testing.T, p *Printer, doc string) string {
	t.Helper()
	var buf bytes.Buffer
	p.Out = &buf
	if err := p.PrintRaw([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestCompactKeepsAPIFormatting(t *testing.T) {
	got := print(t, &Printer{Format: FormatJSON}, `{ "b": 1.50, "a": 2 }`)
	if want := `{"b":1.50,"a":2}` + "\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPretty(t *testing.T) {
	got := print(t, &Printer{Format: FormatJSON, Pretty: true}, `{"a":[1,2]}`)
	want := "{\n  \"a\": [\n    1,\n    2\n  ]\n}\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJSONLSplitsArrays(t *testing.T) {
	got := print(t, &Printer{Format: FormatJSONL, Pretty: true}, hosts)
	want := `{"id":"5Vf7Be7guuy","name":"web-1","createdAt":1793970084,"memo":"<a&b>"}` + "\n" +
		`{"id":"abcdefghijk","name":"web-2","createdAt":1793970085}` + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJQRawStrings(t *testing.T) {
	code, err := CompileJQ(`.[].id`)
	if err != nil {
		t.Fatal(err)
	}
	got := print(t, &Printer{Format: FormatJSON, Query: code}, hosts)
	if want := "5Vf7Be7guuy\nabcdefghijk\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJQKeepsLargeIntegersAndHTML(t *testing.T) {
	code, err := CompileJQ(`{n: .n, m: .m}`)
	if err != nil {
		t.Fatal(err)
	}
	got := print(t, &Printer{Format: FormatJSON, Query: code}, `{"n":12345678901234567890123,"m":"<x>"}`)
	if want := `{"m":"<x>","n":12345678901234567890123}` + "\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJQTSV(t *testing.T) {
	code, err := CompileJQ(`.[] | [.id, .createdAt] | @tsv`)
	if err != nil {
		t.Fatal(err)
	}
	got := print(t, &Printer{Format: FormatJSON, Query: code}, hosts)
	if want := "5Vf7Be7guuy\t1793970084\nabcdefghijk\t1793970085\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJQEnvIsNotExposed(t *testing.T) {
	t.Setenv("MACKEREL_APIKEY", "secret")
	code, err := CompileJQ(`$ENV.MACKEREL_APIKEY`)
	if err != nil {
		t.Fatal(err)
	}
	got := print(t, &Printer{Format: FormatJSON, Query: code}, `{}`)
	if got != "null\n" {
		t.Errorf("got %q", got)
	}
}

func TestColorize(t *testing.T) {
	got := string(Colorize([]byte(`{"k":"v","n":-1.5e3,"t":true,"z":null,"s":"a\"b:"}`)))
	want := "{" + colorKey + `"k"` + colorReset + ":" + colorString + `"v"` + colorReset +
		"," + colorKey + `"n"` + colorReset + ":" + colorNumber + "-1.5e3" + colorReset +
		"," + colorKey + `"t"` + colorReset + ":" + colorBool + "true" + colorReset +
		"," + colorKey + `"z"` + colorReset + ":" + colorNull + "null" + colorReset +
		"," + colorKey + `"s"` + colorReset + ":" + colorString + `"a\"b:"` + colorReset + "}"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestInvalidJSON(t *testing.T) {
	p := &Printer{Out: &bytes.Buffer{}}
	if err := p.PrintRaw([]byte(`{nope`)); err == nil {
		t.Error("want error")
	}
}
