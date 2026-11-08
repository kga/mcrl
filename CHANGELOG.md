# Changelog

## [v0.4.1](https://github.com/kga/mcrl/compare/v0.4.0...v0.4.1) - 2026-11-08

### Dependencies
- Bump github.com/spf13/pflag from 1.0.9 to 1.1.0 in the go-modules group by @dependabot[bot] in https://github.com/kga/mcrl/pull/59
### Other changes
- Test on macOS and Windows in CI and update dependencies weekly by @kga in https://github.com/kga/mcrl/pull/57
- Auto-merge Dependabot patch and minor updates and stop grouping them by @kga in https://github.com/kga/mcrl/pull/60

## [v0.4.0](https://github.com/kga/mcrl/compare/v0.3.0...v0.4.0) - 2026-11-08

### New features
- Exit with 9 when --jq fails after a change was applied by @kga in https://github.com/kga/mcrl/pull/53
### Other changes
- Move the design documents to a private repository by @kga in https://github.com/kga/mcrl/pull/38
- Take the development API key from an mcrl profile by @kga in https://github.com/kga/mcrl/pull/40
- Show when an alert last changed status in triage by @kga in https://github.com/kga/mcrl/pull/50
- Name the monitor of check monitoring alerts in triage by @kga in https://github.com/kga/mcrl/pull/51
- Find the last metric time when /tsdb/latest has no value by @kga in https://github.com/kga/mcrl/pull/49
- Rank triage's disk% filesystems by usage and show them in used/size pairs by @kga in https://github.com/kga/mcrl/pull/52
- Show the API key source in error hints and trim api_key_command failures by @kga in https://github.com/kga/mcrl/pull/54
- Describe array arguments and update-tool output in MCP by @kga in https://github.com/kga/mcrl/pull/55
- Explain in audit's help that host checks cover every status by @kga in https://github.com/kga/mcrl/pull/56

## [v0.3.0](https://github.com/kga/mcrl/compare/v0.2.2...v0.3.0) - 2026-11-07

### New features
- Add mcrl triage for the first minutes of an incident by @kga in https://github.com/kga/mcrl/pull/36
### Other changes
- Resolve triage T2: rate monitor values are not metrics by @kga in https://github.com/kga/mcrl/pull/33
- Record that downtimes stop alerts from being created by @kga in https://github.com/kga/mcrl/pull/35
- Prepare the repository for going public by @kga in https://github.com/kga/mcrl/pull/37

## [v0.2.2](https://github.com/kga/mcrl/compare/v0.2.1...v0.2.2) - 2026-11-07

### Other changes
- Pin a profile per directory with MCRL_PROFILE instead of .mcrl.toml by @kga in https://github.com/kga/mcrl/pull/29
- Split user guides from design documents by @kga in https://github.com/kga/mcrl/pull/31
- Note in the README that mcrl is a proof of concept by @kga in https://github.com/kga/mcrl/pull/32

## [v0.2.1](https://github.com/kga/mcrl/compare/v0.2.0...v0.2.1) - 2026-11-07

### Other changes
- Verify release attestations with attestations: read by @kga in https://github.com/kga/mcrl/pull/21
- Hide secrets in JSON bodies printed by --debug by @kga in https://github.com/kga/mcrl/pull/23
- Propose mcrl triage, the first command to run in an incident by @kga in https://github.com/kga/mcrl/pull/24
- Stop mapping Mackerel services to OpenTelemetry services in triage by @kga in https://github.com/kga/mcrl/pull/25
- Describe log search conditions accurately by @kga in https://github.com/kga/mcrl/pull/26
- Check off how retired hosts look in the API by @kga in https://github.com/kga/mcrl/pull/28
- Keep stored API keys consistent when auth login or logout fails midway by @kga in https://github.com/kga/mcrl/pull/27

## [v0.2.0](https://github.com/kga/mcrl/compare/v0.1.1...v0.2.0) - 2026-11-07

### New features
- Add mcrl mcp to expose commands to AI agents over MCP by @kga in https://github.com/kga/mcrl/pull/17
- Check that every API endpoint has a command and generate the command reference by @kga in https://github.com/kga/mcrl/pull/18
- Add mcrl audit to list forgotten or stale settings by @kga in https://github.com/kga/mcrl/pull/19
- Add extensions that run mcrl-<name> as mcrl <name> by @kga in https://github.com/kga/mcrl/pull/20
### Other changes
- Fail the release when the release attestation cannot be verified by @kga in https://github.com/kga/mcrl/pull/14
- Document where to find the release attestation in the web UI by @kga in https://github.com/kga/mcrl/pull/16

## [v0.1.1](https://github.com/kga/mcrl/compare/v0.1.0...v0.1.1) - 2026-11-06

### Other changes
- Call reusable workflows with $/ and document the PR-based flow by @kga in https://github.com/kga/mcrl/pull/9
- Publish releases automatically for immutable releases by @kga in https://github.com/kga/mcrl/pull/10
- Protect main with a single required check and run tagpr with an App token by @kga in https://github.com/kga/mcrl/pull/12
- Use merge commits instead of squash merges by @kga in https://github.com/kga/mcrl/pull/13

## [v0.1.0](https://github.com/kga/mcrl/commits/v0.1.0) - 2026-11-06

### Dependencies
- Bump the github-actions group with 5 updates by @dependabot[bot] in https://github.com/kga/mcrl/pull/5
