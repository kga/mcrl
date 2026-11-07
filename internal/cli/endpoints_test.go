package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/kga/mcrl/internal/cli/cmdutil"
	"github.com/kga/mcrl/internal/mackerel"
)

// Every endpoint of the Mackerel API has a command, or is listed in
// endpointsLeftToAPI with the reason it has none (docs/decisions.md D21).
func TestEndpointCoverage(t *testing.T) {
	root := NewRootCmd(&cmdutil.Factory{IO: &cmdutil.IOStreams{}})
	uses, errs := endpointCoverage(root)
	for _, err := range errs {
		t.Error(err)
	}
	covered := 0
	for _, u := range uses {
		switch {
		case len(u.Commands) == 0 && u.LeftToAPI == "":
			t.Errorf("%s (%s) has no command: add one that lists it in withAPI, or add it to endpointsLeftToAPI with the reason", u.Endpoint, u.Endpoint.DocURL())
		case len(u.Commands) > 0 && u.LeftToAPI != "":
			t.Errorf("%s is in endpointsLeftToAPI, but %s uses it: remove it from endpointsLeftToAPI", u.Endpoint, u.Commands[0].CommandPath())
		case len(u.Commands) > 0:
			covered++
		}
	}
	for key := range endpointsLeftToAPI {
		method, path, _ := strings.Cut(key, " ")
		if e, ok := mackerel.LookupEndpoint(method, path); !ok || e.String() != key {
			t.Errorf("endpointsLeftToAPI has %q, which is not in mackerel.Endpoints", key)
		}
	}
	t.Logf("%d of %d endpoints have a command; %d are left to mcrl api", covered, len(uses), len(endpointsLeftToAPI))
}

func TestEndpointsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range mackerel.Endpoints {
		key := e.Method + " " + e.Path
		if seen[key] {
			t.Errorf("duplicate endpoint %s", key)
		}
		seen[key] = true
		if e.Section() == "" {
			t.Errorf("%s: unknown page %q", e, e.Doc)
		}
		if !strings.HasPrefix(e.Path, "/api/v0/") {
			t.Errorf("%s: the path must start with /api/v0/", e)
		}
	}
	if found, ok := mackerel.LookupEndpoint("GET", "/api/v0/hosts/<id>?x=1"); !ok || found.Path != "/api/v0/hosts/<hostId>" {
		t.Errorf("LookupEndpoint should match any placeholder and ignore the query: %v %v", found, ok)
	}
}

func TestAPIOf(t *testing.T) {
	long := withAPI("Text.\n\nMore text.", "Read",
		"GET /api/v0/settings/ip-restriction/{web,api}", "GET /api/v0/monitors (--expand)",
		"GET /api/v0/{hosts/<id>,services/<name>}/metadata/<ns>")
	eps, perm := apiOf(long)
	want := []string{
		"GET /api/v0/settings/ip-restriction/web", "GET /api/v0/settings/ip-restriction/api",
		"GET /api/v0/monitors",
		"GET /api/v0/hosts/<id>/metadata/<ns>", "GET /api/v0/services/<name>/metadata/<ns>",
	}
	if !slices.Equal(eps, want) || perm != "Read" {
		t.Errorf("got %q %q", eps, perm)
	}
	if eps, perm := apiOf("No API."); eps != nil || perm != "" {
		t.Errorf("got %q %q", eps, perm)
	}
}
