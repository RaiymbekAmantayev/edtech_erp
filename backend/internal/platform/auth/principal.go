package auth

import (
	"context"
	"slices"
)

// Principal — аутентифицированный пользователь и его права в текущей организации.
// Формируется на сервере из JWT и БД; tenant_id из запроса клиента не используется.
type Principal struct {
	UserID          string
	TenantID        string
	FullName        string
	IsPlatformAdmin bool
	Permissions     map[string]bool
	AllBranches     bool
	BranchIDs       []string
	EmployeeID      string // если пользователь — сотрудник/преподаватель
	RoleCodes       []string
	IP              string
	UserAgent       string
}

func (p *Principal) Can(perm string) bool {
	if p == nil {
		return false
	}
	return p.Permissions[perm]
}

func (p *Principal) HasRole(code string) bool { return slices.Contains(p.RoleCodes, code) }

// BranchFilter возвращает nil, если доступны все филиалы, иначе список разрешённых.
func (p *Principal) BranchFilter() []string {
	if p.AllBranches {
		return nil
	}
	if p.BranchIDs == nil {
		return []string{}
	}
	return p.BranchIDs
}

func (p *Principal) CanBranch(id string) bool {
	return p.AllBranches || slices.Contains(p.BranchIDs, id)
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// Список всех permissions формата module.action.
var AllPermissions = []string{
	"students.view", "students.edit", "students.export", "students.import", "students.view_iin",
	"parents.view", "parents.edit",
	"leads.view", "leads.edit",
	"groups.view", "groups.edit",
	"schedule.view", "schedule.edit",
	"attendance.view", "attendance.mark", "attendance.edit_closed", "tests.conduct",
	"finance.view", "payments.create", "payments.refund", "subscriptions.edit",
	"reports.view",
	"employees.view", "employees.edit",
	"settings.manage", "custom_fields.manage", "roles.manage", "branches.manage",
	"audit.view", "billing.manage",
}

type SystemRole struct {
	Code        string
	Name        string
	AllBranches bool
	Permissions []string
}

func without(all []string, drop ...string) []string {
	out := []string{}
	for _, p := range all {
		if !slices.Contains(drop, p) {
			out = append(out, p)
		}
	}
	return out
}

// SystemRoles — стартовый шаблон матрицы прав (ТЗ 2.8).
var SystemRoles = []SystemRole{
	{"owner", "Владелец", true, AllPermissions},
	{"org_admin", "Администратор", true, without(AllPermissions, "billing.manage")},
	{"branch_manager", "Управляющий филиалом", false, []string{
		"students.view", "students.edit", "students.export", "students.import", "parents.view", "parents.edit",
		"leads.view", "leads.edit", "groups.view", "groups.edit", "schedule.view", "schedule.edit",
		"attendance.view", "attendance.mark", "attendance.edit_closed", "finance.view", "payments.create",
		"subscriptions.edit", "reports.view", "employees.view", "employees.edit", "audit.view", "tests.conduct"}},
	{"academic", "Учебный отдел", true, []string{
		"students.view", "students.edit", "parents.view", "leads.view", "groups.view", "groups.edit",
		"schedule.view", "schedule.edit", "attendance.view", "attendance.mark", "attendance.edit_closed",
		"reports.view", "employees.view", "tests.conduct"}},
	{"accountant", "Бухгалтер", true, []string{
		"students.view", "parents.view", "groups.view", "schedule.view", "attendance.view",
		"finance.view", "payments.create", "payments.refund", "subscriptions.edit", "reports.view",
		"employees.view", "audit.view"}},
	{"hr", "HR-специалист", true, []string{"employees.view", "employees.edit"}},
	{"sales", "Менеджер продаж", false, []string{
		"students.view", "parents.view", "parents.edit", "leads.view", "leads.edit", "groups.view",
		"schedule.view", "payments.create"}},
	{"teacher", "Преподаватель", false, []string{"attendance.mark", "tests.conduct"}},
}
