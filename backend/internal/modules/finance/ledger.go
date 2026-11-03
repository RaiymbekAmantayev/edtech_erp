package finance

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Post записывает проводку и пересчитывает кэш баланса в той же транзакции (ТЗ 15.6.2).
func Post(ctx context.Context, tx pgx.Tx, studentID, kind string, amount int64, refType string, refID *string, comment string, userID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (tenant_id, student_id, kind, amount_tiyn, ref_type, ref_id, comment, created_by)
		VALUES (app_current_tenant(), $1, $2, $3, NULLIF($4,''), $5, NULLIF($6,''), NULLIF($7,'')::uuid)`,
		studentID, kind, amount, refType, refID, comment, userID); err != nil {
		return err
	}
	return RecalcBalance(ctx, tx, studentID)
}

// RecalcBalance: баланс считается из проводок, а student_balances — денормализованный кэш.
func RecalcBalance(ctx context.Context, tx pgx.Tx, studentID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO student_balances (tenant_id, student_id, balance_tiyn, updated_at)
		SELECT app_current_tenant(), $1, coalesce(sum(amount_tiyn),0), NOW() FROM ledger_entries WHERE student_id=$1
		ON CONFLICT (student_id) DO UPDATE SET balance_tiyn=EXCLUDED.balance_tiyn, updated_at=NOW()`, studentID)
	return err
}
