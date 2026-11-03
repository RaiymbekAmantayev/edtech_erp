// Пакет finance — абонементы, заморозки, платежи, возвраты, баланс и должники (ТЗ раздел 15, MVP-объём).
package finance

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

var Methods = []string{"cash", "card", "transfer", "kaspi", "online", "other"}

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("finance.view")).Get("/payments", httpx.Handler(h.payments).ServeHTTP)
	r.With(mw.Perm("finance.view")).Get("/debtors", httpx.Handler(h.debtors).ServeHTTP)
	r.With(mw.Perm("finance.view")).Get("/students/{id}/ledger", httpx.Handler(h.ledger).ServeHTTP)
	r.With(mw.Perm("finance.view")).Get("/finance/summary", httpx.Handler(h.summary).ServeHTTP)
	r.With(mw.Perm("payments.create")).Post("/payments", httpx.Handler(h.createPayment).ServeHTTP)
	r.With(mw.Perm("payments.refund")).Post("/payments/{id}/refund", httpx.Handler(h.refund).ServeHTTP)
	r.With(mw.Perm("payments.refund")).Post("/students/{id}/adjust", httpx.Handler(h.adjust).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("subscriptions.edit"))
		r.Post("/subscriptions", httpx.Handler(h.createSubscription).ServeHTTP)
		r.Post("/subscriptions/{id}/freeze", httpx.Handler(h.freeze).ServeHTTP)
		r.Post("/subscriptions/{id}/cancel", httpx.Handler(h.cancelSubscription).ServeHTTP)
	})
}

// ---------------------------------------------------------------- абонементы

type subReq struct {
	StudentID    string `json:"student_id"`
	CourseID     string `json:"course_id"`
	Type         string `json:"type"` // lessons, monthly, period, unlimited, custom
	Name         string `json:"name"`
	PriceTiyn    int64  `json:"price_tiyn"`
	LessonsTotal *int   `json:"lessons_total"`
	StartsOn     string `json:"starts_on"`
	EndsOn       string `json:"ends_on"`
}

func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) error {
	var req subReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if req.StudentID == "" {
		v.Add("student_id", "REQUIRED")
	}
	if !slices.Contains([]string{"lessons", "monthly", "period", "unlimited", "custom"}, req.Type) {
		v.Add("type", "INVALID")
	}
	if req.PriceTiyn < 0 {
		v.Add("price_tiyn", "INVALID")
	}
	if req.Type == "lessons" && (req.LessonsTotal == nil || *req.LessonsTotal <= 0) {
		v.Add("lessons_total", "REQUIRED")
	}
	start, err := time.Parse("2006-01-02", req.StartsOn)
	if err != nil {
		start = time.Now()
	}
	var end *time.Time
	if req.EndsOn != "" {
		e, err := time.Parse("2006-01-02", req.EndsOn)
		if err != nil || e.Before(start) {
			v.Add("ends_on", "INVALID_DATE")
		}
		end = &e
	} else if req.Type == "monthly" {
		e := start.AddDate(0, 1, -1)
		end = &e
	} else if req.Type == "lessons" {
		e := start.AddDate(0, 0, 45)
		end = &e
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	var id string
	err = h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if req.Name == "" {
			var cname string
			if req.CourseID != "" {
				tx.QueryRow(ctx, `SELECT name FROM courses WHERE id=$1`, req.CourseID).Scan(&cname)
			}
			switch req.Type {
			case "lessons":
				req.Name = strconv.Itoa(*req.LessonsTotal) + " занятий"
			case "monthly":
				req.Name = "Месяц"
			case "unlimited":
				req.Name = "Безлимит"
			default:
				req.Name = "Абонемент"
			}
			if cname != "" {
				req.Name += " · " + cname
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO subscriptions (tenant_id, student_id, course_id, type, name, price_tiyn, lessons_total, starts_on, ends_on, created_by)
			VALUES (app_current_tenant(), $1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			req.StudentID, req.CourseID, req.Type, req.Name, req.PriceTiyn, req.LessonsTotal, start, end, p.UserID).Scan(&id); err != nil {
			return err
		}
		// стоимость абонемента — начисление (минус к балансу); оплата закрывает задолженность
		if req.PriceTiyn > 0 {
			if err := Post(ctx, tx, req.StudentID, "charge", -req.PriceTiyn, "subscription", &id, "Абонемент: "+req.Name, p.UserID); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "student", req.StudentID, "subscription_created", nil, map[string]any{"subscription_id": id, "name": req.Name, "price_tiyn": req.PriceTiyn})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

// freeze — заморозка на период: занятия не списываются, срок абонемента продлевается на длительность заморозки (ТЗ 15.6.1).
func (h *Handler) freeze(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		StartsOn string `json:"starts_on"`
		EndsOn   string `json:"ends_on"`
		Reason   string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	from, err1 := time.Parse("2006-01-02", req.StartsOn)
	to, err2 := time.Parse("2006-01-02", req.EndsOn)
	if err1 != nil || err2 != nil || to.Before(from) {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "ends_on", Code: "INVALID_DATE"}}}).Err()
	}
	days := int(to.Sub(from).Hours()/24) + 1
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var studentID, status string
		if err := tx.QueryRow(ctx, `SELECT student_id, status FROM subscriptions WHERE id=$1 FOR UPDATE`, id).Scan(&studentID, &status); err != nil {
			return err
		}
		if status != "active" && status != "frozen" {
			return httpx.Conflict("SUBSCRIPTION_INACTIVE", "Заморозить можно только действующий абонемент")
		}
		var maxDays, maxCount int
		tx.QueryRow(ctx, `SELECT coalesce((settings->>'freeze_max_days')::int, 30), coalesce((settings->>'freeze_max_count')::int, 2) FROM tenants WHERE id = app_current_tenant()`).Scan(&maxDays, &maxCount)
		var usedDays, count int
		tx.QueryRow(ctx, `SELECT coalesce(sum(ends_on - starts_on + 1),0), count(*) FROM subscription_freezes WHERE subscription_id=$1`, id).Scan(&usedDays, &count)
		if count >= maxCount {
			return httpx.Conflict("FREEZE_LIMIT", "Достигнут лимит заморозок для абонемента")
		}
		if usedDays+days > maxDays {
			return httpx.Conflict("FREEZE_LIMIT", "Превышено максимальное количество дней заморозки ("+strconv.Itoa(maxDays)+")")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_freezes (tenant_id, subscription_id, starts_on, ends_on, reason, created_by) VALUES (app_current_tenant(), $1, $2, $3, $4, $5)`,
			id, from, to, req.Reason, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET ends_on = ends_on + $2::int,
			status = CASE WHEN CURRENT_DATE BETWEEN $3::date AND $4::date THEN 'frozen' ELSE status END WHERE id=$1`, id, days, from, to); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "student", studentID, "subscription_frozen", nil, map[string]any{"subscription_id": id, "from": req.StartsOn, "to": req.EndsOn, "reason": req.Reason})
	})
}

func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Refund bool   `json:"refund_unused"`
		Reason string `json:"reason"`
	}
	httpx.Decode(r, &req)
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var studentID, status string
		var price int64
		var total *int
		var used int
		if err := tx.QueryRow(ctx, `SELECT student_id, status, price_tiyn, lessons_total, coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=s.id),0)
			FROM subscriptions s WHERE id=$1 FOR UPDATE`, id).Scan(&studentID, &status, &price, &total, &used); err != nil {
			return err
		}
		if status == "cancelled" {
			return nil
		}
		tx.Exec(ctx, `UPDATE subscriptions SET status='cancelled' WHERE id=$1`, id)
		if req.Refund && total != nil && *total > 0 && used < *total {
			// неиспользованные занятия возвращаются на баланс корректировкой
			back := price * int64(*total-used) / int64(*total)
			if err := Post(ctx, tx, studentID, "adjustment", back, "subscription", &id, "Отмена абонемента: неиспользованные занятия", p.UserID); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "student", studentID, "subscription_cancelled", nil, map[string]any{"subscription_id": id, "reason": req.Reason})
	})
}

// ---------------------------------------------------------------- платежи

type paymentReq struct {
	StudentID     string         `json:"student_id"`
	AmountTiyn    int64          `json:"amount_tiyn"`
	Method        string         `json:"method"`
	PaidAt        string         `json:"paid_at"`
	PayerParentID string         `json:"payer_parent_id"`
	Comment       string         `json:"comment"`
	ReceiptNumber string         `json:"receipt_number"`
	ExternalID    string         `json:"external_id"`
	CF            map[string]any `json:"custom_fields"`
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) error {
	var req paymentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if req.StudentID == "" {
		v.Add("student_id", "REQUIRED")
	}
	if req.AmountTiyn <= 0 {
		v.Add("amount_tiyn", "INVALID")
	}
	if !slices.Contains(Methods, req.Method) {
		v.Add("method", "INVALID")
	}
	paidAt := time.Now()
	if req.PaidAt != "" {
		t, err := time.Parse(time.RFC3339, req.PaidAt)
		if err != nil {
			if t, err = time.Parse("2006-01-02", req.PaidAt); err != nil {
				v.Add("paid_at", "INVALID_DATE")
			}
		}
		if err == nil {
			paidAt = t
		}
	}
	if paidAt.After(time.Now().Add(24 * time.Hour)) {
		v.Add("paid_at", "IN_FUTURE")
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var resp map[string]any
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		// idempotency key: повтор того же запроса вернёт первый результат и не создаст дубль
		if idemKey != "" {
			var saved []byte
			if err := tx.QueryRow(ctx, `SELECT response FROM idempotency_keys WHERE key=$1`, "payment:"+idemKey).Scan(&saved); err == nil {
				return json.Unmarshal(saved, &resp)
			}
		}
		if req.ExternalID != "" {
			var existing string
			if tx.QueryRow(ctx, `SELECT id FROM payments WHERE external_id=$1`, req.ExternalID).Scan(&existing) == nil {
				resp = map[string]any{"id": existing, "duplicate": true}
				return nil
			}
		}
		var branchID string
		if err := tx.QueryRow(ctx, `SELECT branch_id FROM students WHERE id=$1`, req.StudentID).Scan(&branchID); err != nil {
			return httpx.BadRequest("STUDENT_NOT_FOUND", "Ученик не найден")
		}
		if !p.CanBranch(branchID) {
			return httpx.Forbidden()
		}
		if req.PayerParentID == "" {
			tx.QueryRow(ctx, `SELECT parent_id::text FROM student_parents WHERE student_id=$1 AND is_payer LIMIT 1`, req.StudentID).Scan(&req.PayerParentID)
		}
		cf, _ := json.Marshal(orEmpty(req.CF))
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO payments (tenant_id, student_id, payer_parent_id, amount_tiyn, paid_at, method, received_by, comment, receipt_number, external_id, custom_fields)
			VALUES (app_current_tenant(), $1, NULLIF($2,'')::uuid, $3, $4, $5, $6, NULLIF($7,''), NULLIF($8,''), NULLIF($9,''), $10) RETURNING id`,
			req.StudentID, req.PayerParentID, req.AmountTiyn, paidAt, req.Method, p.UserID, req.Comment, req.ReceiptNumber, req.ExternalID, cf).Scan(&id); err != nil {
			return err
		}
		if err := Post(ctx, tx, req.StudentID, "payment", req.AmountTiyn, "payment", &id, methodLabel(req.Method), p.UserID); err != nil {
			return err
		}
		var balance int64
		tx.QueryRow(ctx, `SELECT balance_tiyn FROM student_balances WHERE student_id=$1`, req.StudentID).Scan(&balance)
		if err := database.Audit(ctx, tx, "student", req.StudentID, "payment", nil, map[string]any{"payment_id": id, "amount_tiyn": req.AmountTiyn, "method": req.Method}); err != nil {
			return err
		}
		if err := database.Publish(ctx, tx, "payment.completed", map[string]any{"payment_id": id, "student_id": req.StudentID, "amount_tiyn": req.AmountTiyn}); err != nil {
			return err
		}
		resp = map[string]any{"id": id, "balance_tiyn": balance}
		if idemKey != "" {
			b, _ := json.Marshal(resp)
			tx.Exec(ctx, `INSERT INTO idempotency_keys (tenant_id, key, response) VALUES (app_current_tenant(), $1, $2)`, "payment:"+idemKey, b)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, resp)
}

func methodLabel(m string) string {
	return map[string]string{"cash": "Наличные", "card": "Карта", "transfer": "Банковский перевод", "kaspi": "Kaspi перевод", "online": "Онлайн-оплата", "other": "Другое"}[m]
}

// refund — частичный или полный возврат, связанный с исходным платежом; платёж не удаляется.
func (h *Handler) refund(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		AmountTiyn int64  `json:"amount_tiyn"`
		Reason     string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var studentID string
		var amount, refunded int64
		if err := tx.QueryRow(ctx, `SELECT student_id, amount_tiyn, coalesce((SELECT sum(amount_tiyn) FROM refunds WHERE payment_id=p.id),0) FROM payments p WHERE id=$1 FOR UPDATE`, id).
			Scan(&studentID, &amount, &refunded); err != nil {
			return err
		}
		if req.AmountTiyn <= 0 || req.AmountTiyn > amount-refunded {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "amount_tiyn", Code: "EXCEEDS_PAYMENT"}}}).Err()
		}
		if strings.TrimSpace(req.Reason) == "" {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "reason", Code: "REQUIRED"}}}).Err()
		}
		var rid string
		if err := tx.QueryRow(ctx, `INSERT INTO refunds (tenant_id, payment_id, amount_tiyn, reason, created_by) VALUES (app_current_tenant(), $1, $2, $3, $4) RETURNING id`,
			id, req.AmountTiyn, req.Reason, p.UserID).Scan(&rid); err != nil {
			return err
		}
		status := "partially_refunded"
		if refunded+req.AmountTiyn == amount {
			status = "refunded"
		}
		tx.Exec(ctx, `UPDATE payments SET status=$2 WHERE id=$1`, id, status)
		if err := Post(ctx, tx, studentID, "refund", -req.AmountTiyn, "refund", &rid, "Возврат: "+req.Reason, p.UserID); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "student", studentID, "refund", nil, map[string]any{"payment_id": id, "amount_tiyn": req.AmountTiyn, "reason": req.Reason})
	})
}

// adjust — ручная корректировка баланса (исправление через корректирующую операцию с историей).
func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		AmountTiyn int64  `json:"amount_tiyn"`
		Comment    string `json:"comment"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.AmountTiyn == 0 || strings.TrimSpace(req.Comment) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите сумму и причину корректировки")
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := Post(r.Context(), tx, id, "adjustment", req.AmountTiyn, "manual", nil, req.Comment, p.UserID); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "student", id, "balance_adjusted", nil, req)
	})
}

func (h *Handler) payments(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	p := auth.FromContext(r.Context())
	from, to := q.Get("from"), q.Get("to")
	if from == "" {
		from = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	out := []map[string]any{}
	var total, count int64
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		args := []any{from, to, q.Get("method"), q.Get("student_id"), p.BranchFilter(), q.Get("branch_id")}
		where := ` WHERE p.paid_at >= $1::date AND p.paid_at < $2::date AND ($3='' OR p.method=$3) AND ($4='' OR p.student_id::text=$4)
			AND ($5::uuid[] IS NULL OR s.branch_id = ANY($5)) AND ($6='' OR s.branch_id::text=$6)`
		tx.QueryRow(r.Context(), `SELECT coalesce(sum(p.amount_tiyn),0), count(*) FROM payments p JOIN students s ON s.id=p.student_id`+where, args...).Scan(&total, &count)
		rows, err := tx.Query(r.Context(), `SELECT p.id, p.student_id, s.full_name, p.amount_tiyn, p.paid_at, p.method, p.status, p.comment, p.receipt_number,
			coalesce(u.full_name,''), coalesce(pr.full_name,''), coalesce((SELECT sum(amount_tiyn) FROM refunds WHERE payment_id=p.id),0)
			FROM payments p JOIN students s ON s.id=p.student_id LEFT JOIN users u ON u.id=p.received_by LEFT JOIN parents pr ON pr.id=p.payer_parent_id`+where+
			` ORDER BY p.paid_at DESC LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, sid, name, method, status, by, payer string
			var amount, refunded int64
			var at time.Time
			var comment, receipt *string
			rows.Scan(&id, &sid, &name, &amount, &at, &method, &status, &comment, &receipt, &by, &payer, &refunded)
			out = append(out, map[string]any{"id": id, "student_id": sid, "student_name": name, "amount_tiyn": amount, "paid_at": at, "method": method,
				"status": status, "comment": comment, "receipt_number": receipt, "received_by": by, "payer": payer, "refunded_tiyn": refunded})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, out, map[string]any{"total_tiyn": total, "count": count})
}

func (h *Handler) debtors(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	out := []map[string]any{}
	var sum int64
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT s.id, s.full_name, s.phone, b.balance_tiyn,
			(SELECT max(paid_at) FROM payments WHERE student_id=s.id),
			(SELECT string_agg(g.name, ', ') FROM group_students gs JOIN groups g ON g.id=gs.group_id WHERE gs.student_id=s.id AND gs.left_at IS NULL),
			(SELECT pr.full_name || ' · ' || coalesce(pr.phone,'') FROM student_parents sp JOIN parents pr ON pr.id=sp.parent_id WHERE sp.student_id=s.id ORDER BY sp.is_payer DESC LIMIT 1),
			(SELECT min(le.created_at) FROM ledger_entries le WHERE le.student_id=s.id AND le.amount_tiyn < 0 AND le.created_at > coalesce((SELECT max(paid_at) FROM payments WHERE student_id=s.id), '-infinity'))
			FROM students s JOIN student_balances b ON b.student_id=s.id
			WHERE b.balance_tiyn < 0 AND s.archived_at IS NULL AND ($1::uuid[] IS NULL OR s.branch_id = ANY($1)) AND ($2='' OR s.branch_id::text=$2)
			ORDER BY b.balance_tiyn`, p.BranchFilter(), r.URL.Query().Get("branch_id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			var phone, groups, payer *string
			var bal int64
			var last, since *time.Time
			rows.Scan(&id, &name, &phone, &bal, &last, &groups, &payer, &since)
			sum += bal
			out = append(out, map[string]any{"id": id, "full_name": name, "phone": phone, "balance_tiyn": bal, "last_payment_at": last, "groups": groups, "payer": payer, "debt_since": since})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, out, map[string]any{"total_tiyn": sum, "count": len(out)})
}

func (h *Handler) ledger(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	var balance int64
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		id := chi.URLParam(r, "id")
		tx.QueryRow(r.Context(), `SELECT coalesce(sum(amount_tiyn),0) FROM ledger_entries WHERE student_id=$1`, id).Scan(&balance)
		rows, err := tx.Query(r.Context(), `SELECT le.id, le.kind, le.amount_tiyn, le.comment, le.created_at, coalesce(u.full_name,'Система'), le.ref_type, le.ref_id
			FROM ledger_entries le LEFT JOIN users u ON u.id=le.created_by WHERE le.student_id=$1 ORDER BY le.created_at DESC, le.id DESC LIMIT 200`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var lid int64
			var kind, by string
			var amount int64
			var comment, refType, refID *string
			var at time.Time
			rows.Scan(&lid, &kind, &amount, &comment, &at, &by, &refType, &refID)
			out = append(out, map[string]any{"id": lid, "kind": kind, "amount_tiyn": amount, "comment": comment, "created_at": at, "user": by, "ref_type": refType, "ref_id": refID})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, out, map[string]any{"balance_tiyn": balance})
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" {
		from = time.Now().Format("2006-01") + "-01"
	}
	if to == "" {
		to = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		bf := p.BranchFilter()
		var income, refunds int64
		tx.QueryRow(r.Context(), `SELECT coalesce(sum(p.amount_tiyn),0) FROM payments p JOIN students s ON s.id=p.student_id WHERE p.paid_at >= $1::date AND p.paid_at < $2::date AND ($3::uuid[] IS NULL OR s.branch_id=ANY($3))`, from, to, bf).Scan(&income)
		tx.QueryRow(r.Context(), `SELECT coalesce(sum(rf.amount_tiyn),0) FROM refunds rf JOIN payments p ON p.id=rf.payment_id JOIN students s ON s.id=p.student_id WHERE rf.created_at >= $1::date AND rf.created_at < $2::date AND ($3::uuid[] IS NULL OR s.branch_id=ANY($3))`, from, to, bf).Scan(&refunds)
		byMethod := map[string]int64{}
		rows, err := tx.Query(r.Context(), `SELECT p.method, sum(p.amount_tiyn) FROM payments p JOIN students s ON s.id=p.student_id WHERE p.paid_at >= $1::date AND p.paid_at < $2::date AND ($3::uuid[] IS NULL OR s.branch_id=ANY($3)) GROUP BY p.method`, from, to, bf)
		if err != nil {
			return err
		}
		for rows.Next() {
			var m string
			var v int64
			rows.Scan(&m, &v)
			byMethod[m] = v
		}
		rows.Close()
		byCourse := []map[string]any{}
		rows, err = tx.Query(r.Context(), `SELECT coalesce(c.name,'Без курса'), sum(-le.amount_tiyn) FROM ledger_entries le
			LEFT JOIN subscriptions sb ON le.ref_type='subscription' AND sb.id=le.ref_id LEFT JOIN courses c ON c.id=sb.course_id
			JOIN students s ON s.id=le.student_id
			WHERE le.kind='charge' AND le.created_at >= $1::date AND le.created_at < $2::date AND ($3::uuid[] IS NULL OR s.branch_id=ANY($3)) GROUP BY 1 ORDER BY 2 DESC`, from, to, bf)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			var v int64
			rows.Scan(&n, &v)
			byCourse = append(byCourse, map[string]any{"course": n, "charged_tiyn": v})
		}
		rows.Close()
		out = map[string]any{"from": from, "to": to, "income_tiyn": income, "refunds_tiyn": refunds, "by_method": byMethod, "by_course": byCourse}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
