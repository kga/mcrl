package credential

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/kga/mcrl/internal/config"
)

type fakeKeyring map[string]string

func (f fakeKeyring) Get(s, u string) (string, error) {
	if v, ok := f[s+"/"+u]; ok {
		return v, nil
	}
	return "", keyring.ErrNotFound
}
func (f fakeKeyring) Set(s, u, p string) error { f[s+"/"+u] = p; return nil }
func (f fakeKeyring) Delete(s, u string) error { delete(f, s+"/"+u); return nil }

func env(m map[string]string) config.Env { return func(k string) string { return m[k] } }

func TestResolveOrder(t *testing.T) {
	ctx := context.Background()
	kr := fakeKeyring{"mcrl/work": "from-keyring"}
	cmd := func(_ context.Context, argv []string) ([]byte, error) {
		if !reflect.DeepEqual(argv, []string{"op", "read", "op://a b/c"}) {
			t.Fatalf("argv = %q", argv)
		}
		return []byte("from-command\n"), nil
	}
	p := &config.Profile{APIKeyCommand: `op read "op://a b/c"`, APIKey: "from-file"}

	tests := []struct {
		name    string
		env     map[string]string
		profile *config.Profile
		want    Result
	}{
		{"env wins", map[string]string{EnvVar: "from-env"}, p, Result{"from-env", SourceEnv}},
		{"command next", nil, p, Result{"from-command", SourceCommand}},
		{"keyring next", nil, &config.Profile{APIKey: "from-file"}, Result{"from-keyring", SourceKeyring}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{Getenv: env(tt.env), Keyring: kr, RunCommand: cmd}
			got, err := r.Resolve(ctx, "work", tt.profile)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("plaintext last", func(t *testing.T) {
		r := &Resolver{Getenv: env(nil), Keyring: fakeKeyring{}}
		got, err := r.Resolve(ctx, "work", &config.Profile{APIKey: "from-file"})
		if err != nil || got != (Result{"from-file", SourceFile}) {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		r := &Resolver{Getenv: env(nil), Keyring: fakeKeyring{}}
		_, err := r.Resolve(ctx, "work", &config.Profile{})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSplitArgs(t *testing.T) {
	tests := map[string][]string{
		`op read op://x`:           {"op", "read", "op://x"},
		`a "b c" 'd e' f\ g`:       {"a", "b c", "d e", "f g"},
		`  spaced   out  `:         {"spaced", "out"},
		`a ""`:                     {"a", ""},
		`'it''s'`:                  {"its"},
		`echo "say \"hi\""`:        {"echo", `say "hi"`},
		`single 'no \escape here'`: {"single", `no \escape here`},
	}
	for in, want := range tests {
		got, err := SplitArgs(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if _, err := SplitArgs(`a "b`); err == nil {
		t.Error("unterminated quote should fail")
	}
}
