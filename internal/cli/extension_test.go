package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/kga/mcrl/internal/cli/cmdutil"
)

func TestSplitArgs(t *testing.T) {
	root := NewRootCmd(&cmdutil.Factory{IO: &cmdutil.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard}})
	tests := []struct {
		args string
		want int
		ok   bool
	}{
		{"", -1, true},
		{"drill a b", 0, true},
		{"--profile work drill", 2, true},
		{"--profile=work drill", 1, true},
		{"-o jsonl drill", 2, true},
		{"-ojsonl drill", 1, true},
		{"--debug drill", 1, true},
		{"-y drill", 1, true},
		{"--help", -1, true},
		{"--profile drill", -1, true}, // drill is the value of --profile
		{"--unknown drill", -1, false},
		{"-yo json drill", -1, false},
		{"-- drill", -1, false},
	}
	for _, tt := range tests {
		got, ok := splitArgs(root, strings.Fields(tt.args))
		if got != tt.want || ok != tt.ok {
			t.Errorf("splitArgs(%q) = %d, %v; want %d, %v", tt.args, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBuiltinNamesAreReserved(t *testing.T) {
	root := NewRootCmd(&cmdutil.Factory{IO: &cmdutil.IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard}})
	for _, n := range []string{"host", "extension", "ext", "help", "completion", "__complete", "exit-codes"} {
		if !isBuiltinName(root, n) {
			t.Errorf("%q should be reserved", n)
		}
	}
	if isBuiltinName(root, "drill") {
		t.Error("drill should not be reserved")
	}
}
