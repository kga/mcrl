package mackerel

import (
	"net/http"
	"regexp"
	"strings"
)

// Permission is the API key permission an endpoint requires.
type Permission int

const (
	PermRead Permission = 1 << iota
	PermWrite

	PermReadWrite = PermRead | PermWrite
)

func (p Permission) String() string {
	switch p {
	case PermRead:
		return "Read"
	case PermWrite:
		return "Write"
	case PermReadWrite:
		return "Read, Write"
	}
	return "none"
}

// Endpoint is one endpoint of the Mackerel API.
type Endpoint struct {
	Method string
	// Path is the path as written in the API documents, with placeholders
	// such as <hostId>.
	Path string
	// Perm is the permission listed under "Required permissions for the
	// API key" in the API documents.
	Perm Permission
	// Doc is the page and anchor under https://mackerel.io/api-docs/entry/.
	Doc string
	// Title is the endpoint's name in the API documents.
	Title string
}

// String returns "METHOD /api/v0/path".
func (e Endpoint) String() string { return e.Method + " " + e.Path }

// DocURL returns the endpoint's page in the API documents.
func (e Endpoint) DocURL() string { return "https://mackerel.io/api-docs/entry/" + e.Doc }

// Section returns the title of the API documents' page the endpoint is on.
func (e Endpoint) Section() string {
	page, _, _ := strings.Cut(e.Doc, "#")
	return endpointSections[page]
}

var placeholder = regexp.MustCompile(`<[^>]*>`)

// key normalizes a method and path for matching: placeholders become "<>"
// and a query string is dropped.
func endpointKey(method, path string) string {
	path, _, _ = strings.Cut(path, "?")
	return method + " " + placeholder.ReplaceAllString(path, "<>")
}

// LookupEndpoint finds the endpoint for a method and a path. Placeholders
// in the path match any placeholder ("/hosts/<id>" finds "/hosts/<hostId>"),
// and a query string is ignored.
func LookupEndpoint(method, path string) (Endpoint, bool) {
	e, ok := endpointIndex[endpointKey(method, path)]
	return e, ok
}

var endpointIndex = func() map[string]Endpoint {
	m := make(map[string]Endpoint, len(Endpoints))
	for _, e := range Endpoints {
		m[endpointKey(e.Method, e.Path)] = e
	}
	return m
}()

// endpointSections maps a page of the API documents to its title.
var endpointSections = map[string]string{
	"services":             "Services",
	"hosts":                "Hosts",
	"host-metrics":         "Host Metrics",
	"service-metrics":      "Service Metrics",
	"check-monitoring":     "Check Monitoring",
	"metadata":             "Metadata",
	"monitors":             "Monitors",
	"downtimes":            "Downtime",
	"channels":             "Notification Channels",
	"notification-groups":  "Notification Groups",
	"alerts":               "Alerts",
	"alert-group-settings": "Alert Group Settings",
	"dashboards":           "Dashboards",
	"graph-annotations":    "Graph Annotations",
	"users":                "Users",
	"invitations":          "Invitations",
	"organizations":        "Organization",
	"aws-integration":      "AWS Integration",
	"logs":                 "Logs",
	"traces":               "Traces",
	"apm":                  "APM",
}

// Endpoints lists every endpoint in the index of https://mackerel.io/api-docs/
// in its order. The permissions are taken from each endpoint's section.
// Every endpoint must be used by a command or be listed, with a reason, as
// one left to "mcrl api" (internal/cli/endpoints.go); a test checks this.
var Endpoints = []Endpoint{
	{http.MethodGet, "/api/v0/services", PermRead, "services#list", "List Services"},
	{http.MethodPost, "/api/v0/services", PermReadWrite, "services#create", "Register Services"},
	{http.MethodDelete, "/api/v0/services/<serviceName>", PermReadWrite, "services#delete", "Delete Services"},
	{http.MethodGet, "/api/v0/services/<serviceName>/roles", PermRead, "services#rolelist", "List Roles"},
	{http.MethodPost, "/api/v0/services/<serviceName>/roles", PermReadWrite, "services#rolecreate", "Register Roles"},
	{http.MethodDelete, "/api/v0/services/<serviceName>/roles/<roleName>", PermReadWrite, "services#roledelete", "Delete Roles"},
	{http.MethodGet, "/api/v0/services/<serviceName>/metric-names", PermRead, "services#metric-names", "List Metric Names"},

	{http.MethodPost, "/api/v0/hosts", PermReadWrite, "hosts#create", "Register Host Information"},
	{http.MethodGet, "/api/v0/hosts/<hostId>", PermRead, "hosts#get", "Get Host Information"},
	{http.MethodGet, "/api/v0/hosts-by-custom-identifier/<customIdentifier>", PermRead, "hosts#get-by-custom-identifier", "Get Host Information By Custom Identifier"},
	{http.MethodPut, "/api/v0/hosts/<hostId>", PermReadWrite, "hosts#update-information", "Update Host Information"},
	{http.MethodPost, "/api/v0/hosts/<hostId>/status", PermReadWrite, "hosts#update-status", "Update Host Status"},
	{http.MethodPost, "/api/v0/hosts/bulk-update-statuses", PermReadWrite, "hosts#bulk-update-statuses", "Bulk Update Host Statuses"},
	{http.MethodPut, "/api/v0/hosts/<hostId>/role-fullnames", PermWrite, "hosts#update-roles", "Update Host Roles"},
	{http.MethodPost, "/api/v0/hosts/<hostId>/retire", PermReadWrite, "hosts#retire", "Retire Hosts"},
	{http.MethodPost, "/api/v0/hosts/bulk-retire", PermReadWrite, "hosts#bulk-retire", "Bulk Retire Hosts"},
	{http.MethodGet, "/api/v0/hosts", PermRead, "hosts#list", "List Hosts"},
	{http.MethodGet, "/api/v0/hosts/<hostId>/metric-names", PermRead, "hosts#metric-names", "List Metric Names"},
	{http.MethodGet, "/api/v0/hosts/<hostId>/monitored-statuses", PermRead, "hosts#monitored-statuses", "List Monitoring Statuses"},

	{http.MethodPost, "/api/v0/tsdb", PermReadWrite, "host-metrics#post", "Post Metrics"},
	{http.MethodGet, "/api/v0/hosts/<hostId>/metrics", PermRead, "host-metrics#get", "Get Host Metrics"},
	{http.MethodGet, "/api/v0/tsdb/latest", PermRead, "host-metrics#get-latest", "Get Latest Metrics"},
	{http.MethodPost, "/api/v0/graph-defs/create", PermReadWrite, "host-metrics#post-graphdef", "Post Graph Definitions"},
	{http.MethodDelete, "/api/v0/graph-defs", PermReadWrite, "host-metrics#delete-graphdef", "Delete Graph Definitions"},

	{http.MethodPost, "/api/v0/services/<serviceName>/tsdb", PermReadWrite, "service-metrics#post", "Post Service Metrics"},
	{http.MethodGet, "/api/v0/services/<serviceName>/metrics", PermRead, "service-metrics#get", "Get Service Metrics"},
	{http.MethodDelete, "/api/v0/services/<serviceName>/graph-defs/<graphName>", PermReadWrite, "service-metrics#delete-graph-def", "Delete Service Metrics Graph Definition"},

	{http.MethodGet, "/api/v0/monitoring/checks", PermRead, "check-monitoring#list", "List Check Monitors"},
	{http.MethodGet, "/api/v0/monitoring/checks/<monitorId>", PermRead, "check-monitoring#retrieve", "Retrieve Check Monitor"},
	{http.MethodPost, "/api/v0/monitoring/checks/report", PermReadWrite, "check-monitoring#post", "Post Monitoring Check Reports"},

	{http.MethodGet, "/api/v0/hosts/<hostId>/metadata/<namespace>", PermRead, "metadata#hostget", "Get Host Metadata"},
	{http.MethodPut, "/api/v0/hosts/<hostId>/metadata/<namespace>", PermReadWrite, "metadata#hostput", "Register/Update Host Metadata"},
	{http.MethodDelete, "/api/v0/hosts/<hostId>/metadata/<namespace>", PermReadWrite, "metadata#hostdelete", "Delete Host Metadata"},
	{http.MethodGet, "/api/v0/hosts/<hostId>/metadata", PermRead, "metadata#hostlist", "List Host Metadata"},
	{http.MethodGet, "/api/v0/services/<serviceName>/metadata/<namespace>", PermRead, "metadata#serviceget", "Get Service Metadata"},
	{http.MethodPut, "/api/v0/services/<serviceName>/metadata/<namespace>", PermReadWrite, "metadata#serviceput", "Register/Update Service Metadata"},
	{http.MethodDelete, "/api/v0/services/<serviceName>/metadata/<namespace>", PermReadWrite, "metadata#servicedelete", "Delete Service Metadata"},
	{http.MethodGet, "/api/v0/services/<serviceName>/metadata", PermRead, "metadata#servicelist", "List Service Metadata"},
	{http.MethodGet, "/api/v0/services/<serviceName>/roles/<roleName>/metadata/<namespace>", PermRead, "metadata#roleget", "Get Role Metadata"},
	{http.MethodPut, "/api/v0/services/<serviceName>/roles/<roleName>/metadata/<namespace>", PermReadWrite, "metadata#roleput", "Register/Update Role Metadata"},
	{http.MethodDelete, "/api/v0/services/<serviceName>/roles/<roleName>/metadata/<namespace>", PermReadWrite, "metadata#roledelete", "Delete Role Metadata"},
	{http.MethodGet, "/api/v0/services/<serviceName>/roles/<roleName>/metadata", PermRead, "metadata#rolelist", "List Role Metadata"},

	{http.MethodPost, "/api/v0/monitors", PermReadWrite, "monitors#create", "Register Monitor Configurations"},
	{http.MethodGet, "/api/v0/monitors", PermRead, "monitors#list", "List Monitor Configurations"},
	{http.MethodGet, "/api/v0/monitors/<monitorId>", PermRead, "monitors#get", "Get Monitor Configurations"},
	{http.MethodPut, "/api/v0/monitors/<monitorId>", PermReadWrite, "monitors#update", "Update Monitor Configurations"},
	{http.MethodDelete, "/api/v0/monitors/<monitorId>", PermReadWrite, "monitors#delete", "Delete Monitor Configurations"},

	{http.MethodPost, "/api/v0/downtimes", PermReadWrite, "downtimes#create", "Register Downtime"},
	{http.MethodGet, "/api/v0/downtimes", PermRead, "downtimes#list", "List Downtime"},
	{http.MethodPut, "/api/v0/downtimes/<downtimeId>", PermReadWrite, "downtimes#update", "Update Downtime"},
	{http.MethodDelete, "/api/v0/downtimes/<downtimeId>", PermReadWrite, "downtimes#delete", "Delete Downtime"},

	{http.MethodGet, "/api/v0/channels", PermRead, "channels#get", "List Notification Channels"},
	{http.MethodPost, "/api/v0/channels", PermReadWrite, "channels#create", "Register Notification Channel"},
	{http.MethodPut, "/api/v0/channels/<channelId>", PermReadWrite, "channels#update", "Update Notification Channel"},
	{http.MethodDelete, "/api/v0/channels/<channelId>", PermReadWrite, "channels#delete", "Delete Notification Channel"},

	{http.MethodPost, "/api/v0/notification-groups", PermReadWrite, "notification-groups#create", "Register Notification Groups"},
	{http.MethodGet, "/api/v0/notification-groups", PermRead, "notification-groups#get", "Get Notification Groups"},
	{http.MethodPut, "/api/v0/notification-groups/<notificationGroupId>", PermReadWrite, "notification-groups#update", "Update Notification Groups"},
	{http.MethodDelete, "/api/v0/notification-groups/<notificationGroupId>", PermReadWrite, "notification-groups#delete", "Delete Notification Groups"},

	{http.MethodGet, "/api/v0/alerts", PermRead, "alerts#list", "List Alerts"},
	{http.MethodGet, "/api/v0/alerts/<alertId>", PermRead, "alerts#get", "Get Alert"},
	{http.MethodPut, "/api/v0/alerts/<alertId>", PermReadWrite, "alerts#update", "Update Alert"},
	{http.MethodPost, "/api/v0/alerts/<alertId>/close", PermReadWrite, "alerts#close", "Close Alerts"},
	{http.MethodGet, "/api/v0/alerts/<alertId>/logs", PermRead, "alerts#logs", "Get Alert Logs"},

	{http.MethodGet, "/api/v0/alert-group-settings", PermRead, "alert-group-settings#list", "List Alert Group Settings"},
	{http.MethodPost, "/api/v0/alert-group-settings", PermReadWrite, "alert-group-settings#create", "Register Alert Group Settings"},
	{http.MethodGet, "/api/v0/alert-group-settings/<alertGroupSettingId>", PermRead, "alert-group-settings#get", "Get Alert Group Settings"},
	{http.MethodPut, "/api/v0/alert-group-settings/<alertGroupSettingId>", PermReadWrite, "alert-group-settings#update", "Update Alert Group Settings"},
	{http.MethodDelete, "/api/v0/alert-group-settings/<alertGroupSettingId>", PermReadWrite, "alert-group-settings#delete", "Delete Alert Group Settings"},

	{http.MethodPost, "/api/v0/dashboards", PermReadWrite, "dashboards#create", "Create Dashboards"},
	{http.MethodGet, "/api/v0/dashboards/<dashboardId>", PermRead, "dashboards#get", "Get Dashboards"},
	{http.MethodPut, "/api/v0/dashboards/<dashboardId>", PermReadWrite, "dashboards#update", "Update Dashboards"},
	{http.MethodDelete, "/api/v0/dashboards/<dashboardId>", PermReadWrite, "dashboards#delete", "Delete Dashboards"},
	{http.MethodGet, "/api/v0/dashboards", PermRead, "dashboards#list", "List Dashboards"},

	{http.MethodPost, "/api/v0/graph-annotations", PermReadWrite, "graph-annotations#create", "Create Graph Annotations"},
	{http.MethodGet, "/api/v0/graph-annotations", PermRead, "graph-annotations#get", "Get Graph Annotations"},
	{http.MethodPut, "/api/v0/graph-annotations/<annotationId>", PermReadWrite, "graph-annotations#update", "Update Graph Annotations"},
	{http.MethodDelete, "/api/v0/graph-annotations/<annotationId>", PermReadWrite, "graph-annotations#delete", "Delete Graph Annotations"},

	{http.MethodGet, "/api/v0/users", PermRead, "users#list", "List Users that are Organization Members"},
	{http.MethodDelete, "/api/v0/users/<userId>", PermReadWrite, "users#delete", "Delete Users that are Organization Members"},

	{http.MethodGet, "/api/v0/invitations", PermRead, "invitations#list", "List Invitations"},
	{http.MethodPost, "/api/v0/invitations", PermReadWrite, "invitations#create", "Create Invitations"},
	{http.MethodPost, "/api/v0/invitations/revoke", PermReadWrite, "invitations#revoke", "Cancel Invitations"},

	{http.MethodGet, "/api/v0/org", PermRead, "organizations#get", "Get Organization Information"},
	{http.MethodPost, "/api/v0/settings/ip-restriction/web", PermReadWrite, "organizations#ip-restriction-webconsole-post", "Register Access Restriction of Web console Configurations"},
	{http.MethodGet, "/api/v0/settings/ip-restriction/web", PermRead, "organizations#ip-restriction-webconsole-get", "Get Access Restriction of Web console Configurations"},
	{http.MethodPost, "/api/v0/settings/ip-restriction/api", PermReadWrite, "organizations#ip-restriction-api-post", "Register Access Restriction of API Configurations"},
	{http.MethodGet, "/api/v0/settings/ip-restriction/api", PermRead, "organizations#ip-restriction-api-get", "Get Access Restriction of API Configurations"},

	{http.MethodGet, "/api/v0/aws-integrations", PermRead, "aws-integration#list", "List AWS Integration Settings"},
	{http.MethodGet, "/api/v0/aws-integrations/<awsIntegrationId>", PermRead, "aws-integration#get", "Get AWS Integration Settings"},
	{http.MethodPost, "/api/v0/aws-integrations", PermReadWrite, "aws-integration#create", "Register AWS Integration Settings"},
	{http.MethodPut, "/api/v0/aws-integrations/<awsIntegrationId>", PermReadWrite, "aws-integration#update", "Update AWS Integration Settings"},
	{http.MethodDelete, "/api/v0/aws-integrations/<awsIntegrationId>", PermReadWrite, "aws-integration#delete", "Delete AWS Integration Settings"},
	{http.MethodPost, "/api/v0/aws-integrations-external-id", PermReadWrite, "aws-integration#generate-external-id", "Generate AWS Integration External ID"},
	{http.MethodGet, "/api/v0/aws-integrations-excludable-metrics", PermRead, "aws-integration#excludable-metrics", "List Excludable Metrics for AWS Integration"},

	{http.MethodPost, "/api/v0/logs", PermRead, "logs#list", "List Logs"},
	{http.MethodGet, "/api/v0/saved-log-searches", PermRead, "logs#list-saved-searches", "List Saved Log Searches"},

	{http.MethodPost, "/api/v0/traces", PermRead, "traces#list", "List Traces"},
	{http.MethodGet, "/api/v0/traces/<traceId>", PermRead, "traces#get", "Get Trace"},

	{http.MethodGet, "/api/v0/apm/http-server-stats", PermRead, "apm#http-server-stats", "Get HTTP Server Statistics"},
	{http.MethodGet, "/api/v0/apm/db-query-stats", PermRead, "apm#db-query-stats", "Get Database Query Statistics"},
}
