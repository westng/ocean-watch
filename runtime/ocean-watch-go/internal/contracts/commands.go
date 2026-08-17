package contracts

import "strings"

// Command is a stable two-token CLI identity.
// The route is deliberately not configurable by a user command line flag.
type Command struct {
	Domain      string
	Action      string
	Description string
}

type Effect string

const (
	EffectLocalRead          Effect = "local_read"
	EffectLocalWrite         Effect = "local_write"
	EffectPublicRead         Effect = "public_read"
	EffectAuthorizationWrite Effect = "authorization_write"
	EffectOfficialRead       Effect = "official_read"
	EffectOnlineWrite        Effect = "online_write"
)

type Capability struct {
	Channel        string
	Effect         Effect
	RequiresSubmit bool
}

func (c Command) Name() string { return c.Domain + " " + c.Action }

// Commands is the single source of truth for parsing, help, routing, and request budgets.
// Keep additions at the end of their domain.
var Commands = []Command{
	{"setup", "doctor", "Check local runtime requirements"},
	{"setup", "init", "Initialize local configuration"},
	{"setup", "validate", "Validate configuration readiness"},
	{"auth", "set-app", "Store app credentials"},
	{"auth", "authorize", "Run local OAuth authorization"},
	{"auth", "status", "Show redacted token status"},
	{"auth", "refresh", "Refresh an access token"},
	{"auth", "sync-accounts", "Sync advertisers"},
	{"auth", "mappings", "Show sanitized advertiser authorization mappings"},
	{"accounts", "list", "List responsible accounts"},
	{"accounts", "add", "Add or update a responsible account"},
	{"accounts", "remove", "Remove a responsible account"},
	{"accounts", "enable", "Enable a responsible account"},
	{"accounts", "disable", "Disable a responsible account"},
	{"accounts", "report", "Query spend for responsible accounts"},
	{"templates", "list", "List Marketing and Qianchuan templates"},
	{"templates", "show", "Show one Marketing or Qianchuan template"},
	{"templates", "create", "Create a channel-specific template"},
	{"templates", "set-copy", "Set copy materials"},
	{"templates", "validate", "Validate business templates"},
	{"templates", "delete", "Delete a business template"},
	{"qc-templates", "list", "List Qianchuan product templates"},
	{"qc-templates", "create", "Create a Qianchuan product template"},
	{"qc-templates", "list-live", "List Qianchuan live templates"},
	{"qc-templates", "create-live", "Create a Qianchuan live template"},
	{"materials", "videos", "Query uploaded videos"},
	{"materials", "creator", "Query creator videos"},
	{"materials", "images", "Query image assets"},
	{"materials", "products", "Query product assets"},
	{"qc-materials", "creator-videos", "Query Qianchuan creator videos"},
	{"qc-materials", "inspect-work", "Inspect public Douyin work links"},
	{"qc-materials", "authorized-creators", "List authorized Qianchuan creators"},
	{"qc-products", "list", "List Qianchuan products"},
	{"qc-products", "search", "Search Qianchuan products"},
	{"qc-plans", "list", "List Qianchuan plans"},
	{"qc-plans", "show", "Show a Qianchuan plan"},
	{"qc-plans", "materials", "List materials in a Qianchuan plan"},
	{"qc-plans", "update-status", "Update Qianchuan plan status"},
	{"qc-plans", "update-budget", "Update Qianchuan plan budget"},
	{"qc-plans", "update-roi", "Update Qianchuan plan ROI target"},
	{"runs", "list", "List local execution runs"},
	{"runs", "show", "Show one local execution run"},
	{"plans", "create", "Create from uploaded materials"},
	{"plans", "create-creator", "Create from creator materials"},
	{"plans", "create-qianchuan", "Create a Qianchuan all-domain plan"},
	{"plans", "batch-qianchuan-works", "Create or append Qianchuan plans from Douyin work links"},
	{"plans", "remove-qianchuan-work", "Remove Qianchuan plan materials by Douyin work link"},
	{"plans", "batch-upload", "Batch uploaded materials"},
	{"plans", "batch-creator", "Batch creator materials"},
	{"plans", "update-project-status", "Update Marketing project status"},
	{"plans", "update-promotion-status", "Update Marketing promotion status"},
	{"plans", "update-budget", "Update Marketing promotion budget"},
	{"plans", "update-bid", "Update Marketing promotion bid"},
	{"plans", "update-roi", "Update Marketing project ROI target"},
	{"reports", "materials", "Material performance"},
	{"reports", "schema", "Available report fields"},
	{"reports", "custom", "Custom report"},
	{"reports", "plans", "Marketing project performance"},
	{"qc-reports", "plans", "Qianchuan all-domain plan performance"},
	{"qc-reports", "materials", "Qianchuan material performance"},
	{"qc-reports", "account", "Qianchuan all-domain and overall account performance"},
	{"qc-reports", "uni-account", "Qianchuan all-domain account performance"},
	{"qc-reports", "schema", "Qianchuan unified report dimensions and metrics"},
	{"qc-reports", "custom", "Qianchuan custom all-domain or overall report"},
	{"qc-reports", "products", "Qianchuan product-dimension performance"},
	{"qc-reports", "rooms", "Qianchuan live-room dimension performance"},
	{"qc-reports", "authors", "Qianchuan Douyin-author dimension performance"},
	{"discover", "projects", "Find projects"},
	{"discover", "promotions", "Find promotions"},
	{"discover", "dpa", "Find DPA assets"},
	{"discover", "events", "Find event assets"},
	{"discover", "deep-bids", "Find deep bid types"},
	{"discover", "goals", "Find optimization goals"},
	{"discover", "cities", "Resolve city identifiers"},
}

func Lookup(domain, action string) (Command, bool) {
	for _, command := range Commands {
		if command.Domain == domain && command.Action == action {
			return command, true
		}
	}
	return Command{}, false
}

func HasDomain(domain string) bool {
	for _, command := range Commands {
		if command.Domain == domain {
			return true
		}
	}
	return false
}

func Names() []string {
	result := make([]string, 0, len(Commands))
	for _, command := range Commands {
		result = append(result, command.Name())
	}
	return result
}

func CapabilityFor(command Command) Capability {
	name := command.Name()
	capability := Capability{Channel: commandChannel(command)}
	if localReadCommands[name] {
		capability.Effect = EffectLocalRead
	} else if localWriteCommands[name] {
		capability.Effect = EffectLocalWrite
	} else if authorizationWriteCommands[name] {
		capability.Effect = EffectAuthorizationWrite
	} else if publicReadCommands[name] {
		capability.Effect = EffectPublicRead
	} else if onlineWriteCommands[name] {
		capability.Effect = EffectOnlineWrite
		capability.RequiresSubmit = true
	} else if officialReadCommands[name] {
		capability.Effect = EffectOfficialRead
	}
	return capability
}

var localReadCommands = map[string]bool{
	"setup doctor": true, "setup validate": true, "auth status": true, "auth mappings": true,
	"accounts list": true, "templates list": true, "templates show": true, "templates validate": true,
	"qc-templates list": true, "qc-templates list-live": true, "runs list": true, "runs show": true,
}

var localWriteCommands = map[string]bool{
	"setup init": true, "accounts add": true, "accounts remove": true, "accounts enable": true,
	"accounts disable": true, "templates create": true, "templates set-copy": true, "templates delete": true,
	"qc-templates create": true, "qc-templates create-live": true,
}

var authorizationWriteCommands = map[string]bool{
	"auth set-app": true, "auth authorize": true, "auth refresh": true, "auth sync-accounts": true,
}

var publicReadCommands = map[string]bool{
	"qc-materials inspect-work": true,
}

var officialReadCommands = map[string]bool{
	"accounts report":  true,
	"materials videos": true, "materials creator": true, "materials images": true, "materials products": true,
	"qc-materials creator-videos": true, "qc-materials authorized-creators": true,
	"qc-products list": true, "qc-products search": true,
	"qc-plans list": true, "qc-plans show": true, "qc-plans materials": true,
	"reports materials": true, "reports schema": true, "reports custom": true, "reports plans": true,
	"qc-reports plans": true, "qc-reports materials": true, "qc-reports account": true,
	"qc-reports uni-account": true, "qc-reports schema": true, "qc-reports custom": true,
	"qc-reports products": true, "qc-reports rooms": true, "qc-reports authors": true,
	"discover projects": true, "discover promotions": true, "discover dpa": true, "discover events": true,
	"discover deep-bids": true, "discover goals": true, "discover cities": true,
}

var onlineWriteCommands = map[string]bool{
	"plans create": true, "plans create-creator": true, "plans create-qianchuan": true,
	"plans batch-qianchuan-works": true, "plans remove-qianchuan-work": true,
	"plans batch-upload": true, "plans batch-creator": true, "plans update-project-status": true,
	"plans update-promotion-status": true, "plans update-budget": true, "plans update-bid": true,
	"plans update-roi": true, "qc-plans update-status": true, "qc-plans update-budget": true,
	"qc-plans update-roi": true,
}

func commandChannel(command Command) string {
	if strings.HasPrefix(command.Domain, "qc-") || strings.Contains(command.Action, "qianchuan") {
		return "qianchuan"
	}
	if command.Domain == "materials" || command.Domain == "reports" || command.Domain == "discover" ||
		command.Action == "create" || command.Action == "create-creator" ||
		strings.HasPrefix(command.Action, "batch-upload") || strings.HasPrefix(command.Action, "batch-creator") ||
		strings.HasPrefix(command.Action, "update-project") || strings.HasPrefix(command.Action, "update-promotion") ||
		command.Action == "update-bid" {
		return "marketing"
	}
	return "shared"
}
