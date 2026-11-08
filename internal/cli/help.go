package cli

import "github.com/spf13/cobra"

func helpTopic(name, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:    name,
		Short:  short,
		Long:   long,
		Hidden: true,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(cmd.Long)
		},
	}
}

func newHelpTopics() []*cobra.Command {
	return []*cobra.Command{
		helpTopic("exit-codes", "Exit codes used by mcrl", `Exit codes:

  0    success
  1    other error (including 5xx responses after retries)
  2    usage error (bad flags or arguments, confirmation needed without --yes)
  3    authentication or authorization error (no API key, HTTP 401, HTTP 403)
  4    not found (HTTP 404, or no resource matched a name)
  5    invalid input or conflict (HTTP 400, 409, 413, or a name matched several resources)
  6    rate limited (HTTP 429 after retries)
  7    network error (connection failure, timeout)
  8    "mcrl audit --fail-on" found something at or above the given severity
  9    the change was applied, but --jq failed on the output (printed unfiltered)
  130  interrupted (Ctrl-C)`),
		helpTopic("environment", "Environment variables read by mcrl", `Environment variables:

  MACKEREL_APIKEY   API key (overrides the key stored for the profile)
  MCRL_API_BASE     API base URL (default https://api.mackerelio.com)
  MCRL_PROFILE      profile to use
  MCRL_CONFIG       path of the config file
  MCRL_CACHE_DIR    cache directory
  MCRL_OUTPUT       default output format (json or jsonl)
  MCRL_COLOR        auto, always or never
  NO_COLOR          disable colors when set
  MCRL_NO_INPUT     never prompt when set
  MCRL_DEBUG        log HTTP requests and responses to stderr
  MCRL_EXTENSIONS_DIR  directory of installed extensions
  HTTPS_PROXY, NO_PROXY  proxy settings`),
	}
}
