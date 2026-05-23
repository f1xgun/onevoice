// Package authz — permissions.go
//
// The typed permission registry. Phase 1 ships the constants and the
// AllPermissions() accessor; Phase 2 adds cache.go, check.go, loader.go to
// this same package (CONTEXT D-10/D-11). All permissions are flat
// resource.action strings matching ^[a-z_]+\.[a-z_]+$ — no wildcards, no
// hierarchy (REQUIREMENTS Out-of-Scope §"Hierarchical / wildcard").
//
// CHANGES TO THIS FILE MUST BE MIRRORED IN THE MIGRATION SEED.
// migrations/postgres/000007_rbac_data_model.up.sql and
// services/api/migrations/000005_rbac_data_model.up.sql each carry a
// hardcoded JSONB array per system role; drift is caught by
// test/integration/system_roles_test.go (Plan H) which queries the
// seeded JSONB and asserts equality with the registry.
package authz

// Permission is a flat resource.action string. The named type lets handlers
// pass typed values to authz.Can() (Phase 2) and gives the JSON encoder a
// stable wire shape via the underlying string.
type Permission string

// PermissionMeta is the registry entry for one permission. Description is a
// short Russian imperative string consumed by the role-editor Info tooltip
// (UI-RBAC-09 D-13). Adding a new permission requires filling Description
// here; the drift test in permissions_test.go fails CI when this is missed.
type PermissionMeta struct {
	Name        Permission `json:"name"`
	Description string     `json:"description"`
}

// PermissionGroup is the response shape returned by GET /api/v1/permissions
// (Plan G). Resource is the lowercase resource segment ("business",
// "members", etc.); Permissions are ordered by verb in registry order
// (read, then mutating verbs).
type PermissionGroup struct {
	Resource    string           `json:"resource"`
	Permissions []PermissionMeta `json:"permissions"`
}

// Permission constants — exported names follow `Perm<Resource><Action>`
// PascalCase. Adding a new permission means: add the const here, add it to
// the appropriate group in AllPermissions, mirror it in the migration seed
// (and re-run the Phase 1 drift test), and grant it to the relevant system
// roles in the seed JSONB arrays.
const (
	// business.*
	PermBusinessRead              Permission = "business.read"
	PermBusinessUpdate            Permission = "business.update"
	PermBusinessDelete            Permission = "business.delete"
	PermBusinessTransferOwnership Permission = "business.transfer_ownership"

	// members.*
	PermMembersRead       Permission = "members.read"
	PermMembersInvite     Permission = "members.invite"
	PermMembersRemove     Permission = "members.remove"
	PermMembersUpdateRole Permission = "members.update_role"

	// roles.*
	PermRolesRead   Permission = "roles.read"
	PermRolesCreate Permission = "roles.create"
	PermRolesUpdate Permission = "roles.update"
	PermRolesDelete Permission = "roles.delete"

	// integrations.*
	PermIntegrationsRead       Permission = "integrations.read"
	PermIntegrationsConnect    Permission = "integrations.connect"
	PermIntegrationsDisconnect Permission = "integrations.disconnect"

	// content.*
	PermContentRead   Permission = "content.read"
	PermContentCreate Permission = "content.create"
	PermContentUpdate Permission = "content.update"
	PermContentDelete Permission = "content.delete"

	// billing.*
	PermBillingRead   Permission = "billing.read"
	PermBillingUpdate Permission = "billing.update"

	// audit.*
	PermAuditRead Permission = "audit.read"
)

// AllPermissions returns the registry grouped by resource, in the order
// {business, members, roles, integrations, content, billing}. Each group's
// permissions are ordered by registry-declaration order (read first, then
// mutating verbs). The handler in Plan G serializes this directly as JSON.
//
// The function returns a fresh slice on every call so callers cannot mutate
// shared state. Cost is negligible: 6 groups × small slices.
func AllPermissions() []PermissionGroup {
	return []PermissionGroup{
		{Resource: "business", Permissions: []PermissionMeta{
			{Name: PermBusinessRead, Description: "Видеть название, описание и настройки организации."},
			{Name: PermBusinessUpdate, Description: "Редактировать название, описание и базовые настройки."},
			{Name: PermBusinessDelete, Description: "Безвозвратно удалить организацию вместе со всеми данными."},
			{Name: PermBusinessTransferOwnership, Description: "Передавать владение другому участнику. Только текущий владелец."},
		}},
		{Resource: "members", Permissions: []PermissionMeta{
			{Name: PermMembersRead, Description: "Видеть список участников и их роли."},
			{Name: PermMembersInvite, Description: "Создавать ссылки-приглашения для новых участников."},
			{Name: PermMembersRemove, Description: "Исключать участников из организации."},
			{Name: PermMembersUpdateRole, Description: "Назначать участникам другую роль. Кроме самих себя."},
		}},
		{Resource: "roles", Permissions: []PermissionMeta{
			{Name: PermRolesRead, Description: "Видеть список ролей и какие у них права."},
			{Name: PermRolesCreate, Description: "Создавать свои роли с особым набором прав."},
			{Name: PermRolesUpdate, Description: "Редактировать свои роли — название, описание, права."},
			{Name: PermRolesDelete, Description: "Удалять свои роли. Если на роли есть участники, потребуется выбрать новую роль для них."},
		}},
		{Resource: "integrations", Permissions: []PermissionMeta{
			{Name: PermIntegrationsRead, Description: "Видеть подключённые платформы и их статус."},
			{Name: PermIntegrationsConnect, Description: "Привязывать новые аккаунты — Telegram, VK, Яндекс.Бизнес."},
			{Name: PermIntegrationsDisconnect, Description: "Отключать привязанные аккаунты."},
		}},
		{Resource: "content", Permissions: []PermissionMeta{
			{Name: PermContentRead, Description: "Видеть посты, отзывы, переписку, задачи."},
			{Name: PermContentCreate, Description: "Создавать посты, отвечать на отзывы, ставить задачи."},
			{Name: PermContentUpdate, Description: "Редактировать существующие посты, ответы, задачи."},
			{Name: PermContentDelete, Description: "Удалять посты, ответы, задачи."},
		}},
		{Resource: "billing", Permissions: []PermissionMeta{
			{Name: PermBillingRead, Description: "Видеть тариф, счета, использование лимитов."},
			{Name: PermBillingUpdate, Description: "Менять тариф, реквизиты, способ оплаты."},
		}},
		{Resource: "audit", Permissions: []PermissionMeta{
			{Name: PermAuditRead, Description: "Видеть журнал событий — изменения ролей, входы, подключение интеграций."},
		}},
	}
}
