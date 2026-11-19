package authz

import "github.com/MAX-API-Next/MAX-API/common"

// Permission identifies one action on one protected resource.
type Permission struct {
	Resource string
	Action   string
}

// ActionDefinition is the stable, client-facing description of an action.
// DefaultAdmin controls the legacy administrator baseline. Root is an
// implicit superuser and does not need an explicit grant for any action.
type ActionDefinition struct {
	Action         string `json:"action"`
	LabelKey       string `json:"label_key"`
	DescriptionKey string `json:"description_key"`
	DefaultAdmin   bool   `json:"default_admin"`
}

// ResourceDefinition describes the actions exposed by a resource.
type ResourceDefinition struct {
	Resource string             `json:"resource"`
	LabelKey string             `json:"label_key"`
	Actions  []ActionDefinition `json:"actions"`
}

type RoleDescriptor struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	BuiltIn   bool           `json:"built_in"`
	Superuser bool           `json:"superuser"`
	Grants    PermissionsMap `json:"grants"`
}

type PermissionsMap map[string]map[string]bool

const (
	ResourceUser         = "user"
	ResourceChannel      = "channel"
	ResourceSubscription = "subscription"
	ResourceOption       = "option"
	ResourceCustomOAuth  = "custom_oauth"
	ResourcePerformance  = "performance"
	ResourceSmartOps     = "smart_ops"
	ResourceLog          = "log"
	ResourceData         = "data"
	ResourceGroup        = "group"
	ResourcePrefillGroup = "prefill_group"
	ResourceMidjourney   = "midjourney"
	ResourceTask         = "task"
	ResourceVendor       = "vendor"
	ResourceModel        = "model"
	ResourceDeployment   = "deployment"
	ResourceRatioSync    = "ratio_sync"
	ResourceSystem       = "system"
	ResourceTaskPlugin   = "task_plugin"
)

const (
	ActionRead           = "read"
	ActionSearch         = "search"
	ActionOperate        = "operate"
	ActionWrite          = "write"
	ActionSensitiveWrite = "sensitive_write"
	ActionSecretView     = "secret_view"
	ActionDelete         = "delete"
	ActionBind           = "bind"
)

var (
	ChannelRead = Permission{Resource: ResourceChannel, Action: ActionRead}
)

var registry = []ResourceDefinition{
	resource(ResourceUser, "User Management", action(ActionRead, "Read users", "View user records without changing them.", true), action(ActionSearch, "Search users", "Search users and inspect administrative summaries.", true), action(ActionWrite, "Edit users", "Create, update, enable, disable, or delete non-root users.", true), action(ActionSensitiveWrite, "Edit sensitive user settings", "Change roles, credentials, or other sensitive identity settings.", false)),
	resource(ResourceChannel, "Channel Management", action(ActionRead, "Read channels", "View channel lists and details without secrets.", true), action(ActionOperate, "Operate channels", "Test, refresh, enable, or disable channels.", true), action(ActionWrite, "Edit channel routing", "Edit non-sensitive channel routing settings.", true), action(ActionSensitiveWrite, "Edit sensitive channel settings", "Create channels or edit keys and upstream credentials.", false), action(ActionSecretView, "View channel secrets", "View complete channel keys after secure verification.", false)),
	resource(ResourceSubscription, "Subscription Management", action(ActionRead, "Read subscriptions", "View plans and user subscriptions.", true), action(ActionOperate, "Operate subscriptions", "Bind, reset, or invalidate subscriptions.", true), action(ActionWrite, "Edit subscription plans", "Create or edit subscription plans.", true)),
	resource(ResourceOption, "System Options", action(ActionRead, "Read options", "View non-secret system options.", false), action(ActionWrite, "Edit options", "Change system options and payment configuration.", false)),
	resource(ResourceCustomOAuth, "Custom OAuth", action(ActionRead, "Read OAuth providers", "View configured custom OAuth providers.", false), action(ActionWrite, "Edit OAuth providers", "Create, update, or delete custom OAuth providers.", false)),
	resource(ResourcePerformance, "Performance", action(ActionRead, "Read performance", "Inspect performance metrics and logs.", false), action(ActionOperate, "Operate performance", "Reset metrics, run GC, or clean log files.", false)),
	resource(ResourceSmartOps, "Smart Operations", action(ActionRead, "Read operations", "View operational alerts and reconciliation data.", true), action(ActionWrite, "Review operations", "Review or change operational controls.", false)),
	resource(ResourceLog, "Logs", action(ActionRead, "Read logs", "View administrative request and usage logs.", true), action(ActionSearch, "Search logs", "Search administrative logs.", true), action(ActionDelete, "Delete logs", "Delete or clean up logs.", false)),
	resource(ResourceData, "Usage Data", action(ActionRead, "Read usage data", "View aggregate usage and quota data.", true)),
	resource(ResourceGroup, "Groups", action(ActionRead, "Read groups", "View configured groups.", true), action(ActionWrite, "Edit groups", "Change group configuration.", true)),
	resource(ResourcePrefillGroup, "Prefill Groups", action(ActionRead, "Read prefill groups", "View prefill groups.", true), action(ActionWrite, "Edit prefill groups", "Create or edit prefill groups.", true)),
	resource(ResourceMidjourney, "Midjourney Tasks", action(ActionRead, "Read Midjourney tasks", "View administrative Midjourney tasks.", true), action(ActionWrite, "Edit Midjourney tasks", "Manage administrative Midjourney tasks.", true)),
	resource(ResourceTask, "Tasks", action(ActionRead, "Read tasks", "View administrative tasks.", true)),
	resource(ResourceVendor, "Vendors", action(ActionRead, "Read vendors", "View vendor metadata.", true), action(ActionWrite, "Edit vendors", "Create, update, or delete vendor metadata.", true)),
	resource(ResourceModel, "Models", action(ActionRead, "Read models", "View model metadata.", true), action(ActionWrite, "Edit models", "Create, update, or delete model metadata.", true), action(ActionOperate, "Operate model sync", "Preview or apply upstream model synchronization.", true)),
	resource(ResourceDeployment, "Deployments", action(ActionRead, "Read deployments", "View model deployments and locations.", true), action(ActionWrite, "Edit deployments", "Change model deployment configuration.", true), action(ActionOperate, "Operate deployments", "Test deployment connections.", true)),
	resource(ResourceRatioSync, "Ratio Sync", action(ActionRead, "Read ratio sync", "View ratio synchronization targets.", false), action(ActionOperate, "Run ratio sync", "Fetch and apply upstream ratios.", false)),
	resource(ResourceSystem, "System", action(ActionRead, "Read system", "View system and instance information.", false), action(ActionOperate, "Operate system", "Run system maintenance operations.", false)),
	resource(ResourceTaskPlugin, "Task Plugins", action(ActionBind, "Bind task plugins", "Bind registered task plugins to channels.", false)),
}

func resource(name, label string, actions ...ActionDefinition) ResourceDefinition {
	return ResourceDefinition{Resource: name, LabelKey: label, Actions: actions}
}

func action(name, label, description string, defaultAdmin bool) ActionDefinition {
	return ActionDefinition{Action: name, LabelKey: label, DescriptionKey: description, DefaultAdmin: defaultAdmin}
}

func Catalog() []ResourceDefinition {
	result := make([]ResourceDefinition, 0, len(registry))
	for _, item := range registry {
		result = append(result, ResourceDefinition{
			Resource: item.Resource,
			LabelKey: item.LabelKey,
			Actions:  append([]ActionDefinition(nil), item.Actions...),
		})
	}
	return result
}

func AllPermissions() []Permission {
	result := make([]Permission, 0)
	for _, item := range registry {
		for _, entry := range item.Actions {
			result = append(result, Permission{Resource: item.Resource, Action: entry.Action})
		}
	}
	return result
}

func actionDefinition(permission Permission) (ActionDefinition, bool) {
	for _, item := range registry {
		if item.Resource != permission.Resource {
			continue
		}
		for _, entry := range item.Actions {
			if entry.Action == permission.Action {
				return entry, true
			}
		}
	}
	return ActionDefinition{}, false
}

func isKnownPermission(permission Permission) bool {
	_, ok := actionDefinition(permission)
	return ok
}

func roleGrants(role int) PermissionsMap {
	result := make(PermissionsMap, len(registry))
	for _, item := range registry {
		actions := make(map[string]bool, len(item.Actions))
		for _, entry := range item.Actions {
			actions[entry.Action] = role >= common.RoleRootUser || (role >= common.RoleAdminUser && entry.DefaultAdmin)
		}
		result[item.Resource] = actions
	}
	return result
}

func Roles() []RoleDescriptor {
	return []RoleDescriptor{
		{Key: "root", Name: "Root", BuiltIn: true, Superuser: true, Grants: roleGrants(common.RoleRootUser)},
		{Key: "admin", Name: "Admin", BuiltIn: true, Superuser: false, Grants: roleGrants(common.RoleAdminUser)},
		{Key: "user", Name: "User", BuiltIn: true, Superuser: false, Grants: roleGrants(common.RoleCommonUser)},
	}
}
