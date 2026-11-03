-- Роль приложения emp_app: не владелец таблиц и без BYPASSRLS, поэтому RLS применяется ко всем её запросам.
-- Миграции и системные worker'ы работают под emp_owner (владелец таблиц, RLS к нему не применяется).

GRANT USAGE ON SCHEMA public TO emp_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO emp_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO emp_app;

-- Журналы и проводки нельзя редактировать или удалять из приложения.
REVOKE UPDATE, DELETE ON audit_logs, access_logs, attendance_history, ledger_entries, subscription_usages, export_logs FROM emp_app;
-- Слияние дубликатов переносит списания на основную запись — разрешено менять только student_id.
GRANT UPDATE (student_id) ON subscription_usages TO emp_app;
-- Платежи и возвраты физически не удаляются.
REVOKE DELETE ON payments, refunds FROM emp_app;
-- Платформенные таблицы.
REVOKE ALL ON sessions, tenant_billing_payments FROM emp_app;
REVOKE INSERT, UPDATE, DELETE ON subscription_plans FROM emp_app;
REVOKE INSERT, DELETE ON tenants FROM emp_app;

-- Таблицы из будущих миграций получают те же права автоматически.
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO emp_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO emp_app;
