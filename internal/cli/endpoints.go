package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kga/mcrl/internal/mackerel"
)

// endpointsLeftToAPI lists, with the reason, the endpoints that have no
// command on purpose and are called with "mcrl api". The keys are
// mackerel.Endpoint.String().
var endpointsLeftToAPI = map[string]string{}

// endpointUse is an endpoint and the commands whose help lists it.
type endpointUse struct {
	Endpoint  mackerel.Endpoint
	Commands  []*cobra.Command
	LeftToAPI string // the reason it has no command, from endpointsLeftToAPI
}

// endpointCoverage maps every endpoint in mackerel.Endpoints, in order, to
// the commands whose help lists it (see withAPI). It also reports help texts
// that list an endpoint the API does not have, or a permission that differs
// from the one the endpoints require.
func endpointCoverage(root *cobra.Command) ([]endpointUse, []error) {
	byEndpoint := map[string][]*cobra.Command{}
	var errs []error
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		eps, declared := apiOf(cmd.Long)
		var perm mackerel.Permission
		for _, s := range eps {
			method, path, _ := strings.Cut(s, " ")
			e, ok := mackerel.LookupEndpoint(method, path)
			if !ok {
				errs = append(errs, fmt.Errorf("%s: the help lists %q, which is not in mackerel.Endpoints", cmd.CommandPath(), s))
				continue
			}
			perm |= e.Perm
			if list := byEndpoint[e.String()]; len(list) == 0 || list[len(list)-1] != cmd {
				byEndpoint[e.String()] = append(list, cmd)
			}
		}
		if len(eps) > 0 && perm != 0 && declared != perm.String() {
			errs = append(errs, fmt.Errorf("%s: the help says the API key permission is %q, but its endpoints require %q", cmd.CommandPath(), declared, perm))
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)

	uses := make([]endpointUse, len(mackerel.Endpoints))
	for i, e := range mackerel.Endpoints {
		uses[i] = endpointUse{Endpoint: e, Commands: byEndpoint[e.String()], LeftToAPI: endpointsLeftToAPI[e.String()]}
	}
	return uses, errs
}
