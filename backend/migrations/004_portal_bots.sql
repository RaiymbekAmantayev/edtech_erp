-- Telegram-боты школ и личные кабинеты (Mini App) учеников, родителей и преподавателей.

-- ИИН родителя — логин в кабинет; бот, через который привязан чат (chat_id действителен только для своего бота).
ALTER TABLE parents ADD COLUMN iin VARCHAR(12);
ALTER TABLE parents ADD COLUMN telegram_bot_id BIGINT;
CREATE INDEX parents_iin_idx ON parents (tenant_id, iin) WHERE iin IS NOT NULL;
CREATE INDEX students_iin_idx ON students (tenant_id, iin) WHERE iin IS NOT NULL;

-- Telegram ID сотрудника уникален в пределах организации: преподаватель может работать в нескольких школах.
DROP INDEX users_telegram_uq;
CREATE UNIQUE INDEX users_telegram_uq ON users (tenant_id, telegram_id) WHERE telegram_id IS NOT NULL;

-- Собственный бот школы. Токен никогда не отдаётся в API.
CREATE TABLE tenant_bots (
    tenant_id    UUID PRIMARY KEY REFERENCES tenants(id),
    token        TEXT NOT NULL,
    bot_id       BIGINT NOT NULL UNIQUE,
    username     VARCHAR(64) NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    welcome_text TEXT,
    notify       JSONB NOT NULL DEFAULT '{}', -- {"attendance": false, …} — выключенные типы уведомлений
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Учётные записи кабинета ученика/родителя: вход по ИИН, пароль по умолчанию 12345.
CREATE TABLE portal_accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    kind          VARCHAR(10) NOT NULL CHECK (kind IN ('student', 'parent')),
    student_id    UUID,
    parent_id     UUID,
    iin           VARCHAR(12) NOT NULL,
    password_hash TEXT NOT NULL,
    telegram_id   BIGINT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    failed_logins INT NOT NULL DEFAULT 0,
    locked_until  TIMESTAMPTZ,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, kind, iin),
    CHECK ((kind = 'student') = (student_id IS NOT NULL) AND (kind = 'parent') = (parent_id IS NOT NULL)),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id),
    FOREIGN KEY (tenant_id, parent_id) REFERENCES parents(tenant_id, id)
);
CREATE INDEX portal_accounts_tg_idx ON portal_accounts (tenant_id, telegram_id) WHERE telegram_id IS NOT NULL;

-- Одноразовые приглашения преподавателей в кабинет: t.me/<бот>?start=inv_<code>
CREATE TABLE telegram_invites (
    code        VARCHAR(32) PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    employee_id UUID NOT NULL,
    created_by  UUID,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, employee_id) REFERENCES employees(tenant_id, id)
);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['tenant_bots', 'portal_accounts', 'telegram_invites'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant())', t);
    END LOOP;
END $$;
