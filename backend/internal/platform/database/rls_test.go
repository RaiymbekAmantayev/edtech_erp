package database

import (
	"context"
	"os"
	"testing"

	"emp/internal/platform/config"

	"github.com/jackc/pgx/v5"
)

// Интеграционный тест изоляции организаций (ТЗ 4.2): запускается при наличии локальной БД.
// go test ./internal/platform/database -run RLS
func TestRLSIsolation(t *testing.T) {
	os.Chdir("../../..") // .env лежит в корне backend
	cfg := config.Load()
	ctx := context.Background()
	db, err := Connect(ctx, cfg.DatabaseURL, cfg.SystemURL)
	if err != nil {
		t.Skip("БД недоступна: ", err)
	}
	defer db.Close()

	// две временные организации, созданные системной ролью
	var a, b string
	mk := func(name string) string {
		var id string
		if err := db.Sys.QueryRow(ctx, `INSERT INTO tenants (name, plan_code) VALUES ($1, 'starter') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b = mk("rls-test-A"), mk("rls-test-B")
	t.Cleanup(func() {
		for _, id := range []string{a, b} {
			db.Sys.Exec(ctx, `DELETE FROM students WHERE tenant_id=$1`, id)
			db.Sys.Exec(ctx, `DELETE FROM branches WHERE tenant_id=$1`, id)
			db.Sys.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
		}
	})
	var branchA, studentA string
	err = db.InTenant(ctx, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO branches (tenant_id, name) VALUES ($1,'A') RETURNING id`, a).Scan(&branchA); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO students (tenant_id, branch_id, full_name) VALUES ($1,$2,'Ученик A') RETURNING id`, a, branchA).Scan(&studentA)
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Без app.tenant_id роль приложения не видит ни одной строки (fail-closed).
	var n int
	if err := db.App.QueryRow(ctx, `SELECT count(*) FROM students`).Scan(&n); err != nil || n != 0 {
		t.Errorf("без tenant-контекста видно %d строк (err=%v)", n, err)
	}
	// 2. Организация B не видит ученика A ни списком, ни по ID.
	db.InTenant(ctx, b, func(tx pgx.Tx) error {
		tx.QueryRow(ctx, `SELECT count(*) FROM students WHERE id=$1`, studentA).Scan(&n)
		return nil
	})
	if n != 0 {
		t.Error("организация B видит ученика организации A")
	}
	// 3. B не может записать строку с чужим tenant_id (WITH CHECK).
	err = db.InTenant(ctx, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO students (tenant_id, branch_id, full_name) VALUES ($1,$2,'чужой')`, a, branchA)
		return err
	})
	if err == nil {
		t.Error("вставка с чужим tenant_id должна быть запрещена политикой RLS")
	}
	// 4. B не может сослаться на филиал A даже со своим tenant_id — составной FK (tenant_id, branch_id).
	err = db.InTenant(ctx, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO students (tenant_id, branch_id, full_name) VALUES ($1,$2,'ссылка на чужой филиал')`, b, branchA)
		return err
	})
	if err == nil {
		t.Error("ссылка на филиал другой организации должна быть отклонена составным FK")
	}
	// 5. UPDATE/DELETE чужих строк не затрагивают ничего.
	db.InTenant(ctx, b, func(tx pgx.Tx) error {
		tag, _ := tx.Exec(ctx, `UPDATE students SET full_name='взлом' WHERE id=$1`, studentA)
		n = int(tag.RowsAffected())
		return nil
	})
	if n != 0 {
		t.Error("организация B изменила ученика A")
	}
	// 6. Контекст не «протекает» между транзакциями пула.
	db.InTenant(ctx, a, func(tx pgx.Tx) error { return nil })
	if err := db.App.QueryRow(ctx, `SELECT count(*) FROM students`).Scan(&n); err != nil || n != 0 {
		t.Errorf("tenant-контекст сохранился после транзакции: %d строк", n)
	}
}
