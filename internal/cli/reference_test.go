package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// docs/reference is what WriteReference generates from the current command
// tree (CI runs this test, so a change to a command has to regenerate it).
func TestReferenceUpToDate(t *testing.T) {
	const dir = "../../docs/reference"
	want, err := referenceFiles()
	if err != nil {
		t.Fatal(err)
	}
	have, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range have {
		if _, ok := want[filepath.Base(p)]; !ok {
			t.Errorf("docs/reference/%s is not generated anymore", filepath.Base(p))
		}
	}
	for name, b := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("docs/reference/%s: %v", name, err)
			continue
		}
		if string(got) != string(b) {
			t.Errorf("docs/reference/%s is out of date", name)
		}
	}
	if t.Failed() {
		t.Log(`run "mise run docs" (or "go generate ./internal/cli") and commit docs/reference`)
	}
}
