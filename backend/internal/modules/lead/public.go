package lead

import (
	"net/http"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PublicForm — приём заявки с сайта организации (без авторизации, с ограничением частоты).
// Tenant берётся из пути, а не из тела запроса; поля, разрешённые форме, — только стандартные и web-form доп. поля.
func PublicForm(w http.ResponseWriter, r *http.Request, db *database.DB) error {
	tenantID := chi.URLParam(r, "tenantID")
	if _, err := uuid.Parse(tenantID); err != nil {
		return httpx.NotFound("FORM_NOT_FOUND")
	}
	var exists bool
	db.Sys.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM tenants WHERE id=$1 AND status <> 'suspended')`, tenantID).Scan(&exists)
	if !exists {
		return httpx.NotFound("FORM_NOT_FOUND")
	}
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	src := "Сайт"
	if in.Source == nil || *in.Source == "" {
		in.Source = &src
	}
	in.ResponsibleID, in.Stage, in.LostReason, in.BranchID = nil, nil, nil, nil
	p := &auth.Principal{TenantID: tenantID, Permissions: map[string]bool{}, RoleCodes: []string{"web_form"}}
	ctx := auth.WithPrincipal(r.Context(), p)
	id, _, err := CreateLead(r.WithContext(ctx), db, in, p)
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}
