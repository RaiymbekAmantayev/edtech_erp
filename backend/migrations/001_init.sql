-- EMP: начальная схема. Деньги — BIGINT в тиын, время — TIMESTAMPTZ (UTC).
-- Все бизнес-таблицы содержат tenant_id и защищены RLS (политика tenant_isolation).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE OR REPLACE FUNCTION app_current_tenant() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

-- ---------------------------------------------------------------- платформа
CREATE TABLE subscription_plans (
    code              VARCHAR(30) PRIMARY KEY,
    name              VARCHAR(100) NOT NULL,
    price_tiyn        BIGINT NOT NULL,
    max_students      INT,
    max_branches      INT,
    max_staff         INT,
    max_custom_fields INT NOT NULL,
    storage_gb        INT NOT NULL,
    sms_per_month     INT NOT NULL,
    sort_order        INT NOT NULL DEFAULT 0
);

CREATE TABLE tenants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(255) NOT NULL,
    center_type   VARCHAR(30)  NOT NULL DEFAULT 'other',
    city          VARCHAR(100),
    bin           VARCHAR(12),
    timezone      VARCHAR(64)  NOT NULL DEFAULT 'Asia/Almaty',
    locale        VARCHAR(5)   NOT NULL DEFAULT 'ru',
    currency      CHAR(3)      NOT NULL DEFAULT 'KZT',
    settings      JSONB        NOT NULL DEFAULT '{}',
    plan_code     VARCHAR(30)  NOT NULL REFERENCES subscription_plans(code),
    status        VARCHAR(20)  NOT NULL DEFAULT 'trial', -- trial, active, past_due, over_limit, suspended
    billing_cycle VARCHAR(10)  NOT NULL DEFAULT 'monthly',
    trial_ends_at TIMESTAMPTZ,
    paid_until    DATE,
    over_limit_since TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE tenant_billing_payments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    amount_tiyn BIGINT NOT NULL,
    method     VARCHAR(30) NOT NULL,
    period_to  DATE NOT NULL,
    comment    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE users (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID REFERENCES tenants(id),
    email             VARCHAR(255) NOT NULL,
    phone             VARCHAR(20),
    full_name         VARCHAR(255) NOT NULL,
    password_hash     TEXT NOT NULL,
    is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    locale            VARCHAR(5) NOT NULL DEFAULT 'ru',
    telegram_id       BIGINT,
    status            VARCHAR(20) NOT NULL DEFAULT 'active',
    mfa_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    failed_logins     INT NOT NULL DEFAULT 0,
    locked_until      TIMESTAMPTZ,
    last_login_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);
CREATE UNIQUE INDEX users_email_uq ON users (lower(email));
CREATE UNIQUE INDEX users_telegram_uq ON users (telegram_id) WHERE telegram_id IS NOT NULL;

CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    refresh_hash BYTEA NOT NULL UNIQUE,
    user_agent   TEXT,
    ip           VARCHAR(64),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------- организация
CREATE TABLE branches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    name       VARCHAR(255) NOT NULL,
    address    VARCHAR(500),
    phone      VARCHAR(20),
    timezone   VARCHAR(64) NOT NULL DEFAULT 'Asia/Almaty',
    work_hours JSONB NOT NULL DEFAULT '{"from":"08:00","to":"21:00"}',
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

CREATE TABLE classrooms (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL,
    branch_id  UUID NOT NULL,
    name       VARCHAR(100) NOT NULL,
    capacity   INT,
    is_online  BOOLEAN NOT NULL DEFAULT FALSE,
    equipment  VARCHAR(500),
    status     VARCHAR(20) NOT NULL DEFAULT 'active', -- active, repair
    sort_order INT NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id)
);

CREATE TABLE roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    code        VARCHAR(50) NOT NULL,
    name        VARCHAR(100) NOT NULL,
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    all_branches BOOLEAN NOT NULL DEFAULT FALSE,
    permissions TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code),
    UNIQUE (tenant_id, id)
);

CREATE TABLE user_roles (
    tenant_id UUID NOT NULL,
    user_id   UUID NOT NULL,
    role_id   UUID NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES roles(tenant_id, id)
);

CREATE TABLE user_branch_access (
    tenant_id UUID NOT NULL,
    user_id   UUID NOT NULL,
    branch_id UUID NOT NULL,
    PRIMARY KEY (user_id, branch_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id)
);

-- Глобальные праздники РК: tenant_id IS NULL (ведёт Super Admin).
CREATE TABLE calendar_holidays (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID REFERENCES tenants(id),
    branch_id  UUID,
    day        DATE NOT NULL,
    name       VARCHAR(255) NOT NULL,
    is_workday BOOLEAN NOT NULL DEFAULT FALSE -- перенесённый рабочий день
);
CREATE INDEX calendar_holidays_day ON calendar_holidays (day);

-- ---------------------------------------------------------------- пользовательские поля
CREATE TABLE custom_field_blocks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    entity_type VARCHAR(50) NOT NULL,
    name        JSONB NOT NULL,
    sort_order  INT NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, id)
);

CREATE TABLE custom_field_definitions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    entity_type   VARCHAR(50)  NOT NULL,
    key           VARCHAR(64)  NOT NULL,
    field_type    VARCHAR(30)  NOT NULL,
    label         JSONB        NOT NULL,
    block_id      UUID,
    sort_order    INT          NOT NULL DEFAULT 0,
    settings      JSONB        NOT NULL DEFAULT '{}',
    is_sensitive  BOOLEAN      NOT NULL DEFAULT FALSE,
    is_unique     BOOLEAN      NOT NULL DEFAULT FALSE,
    archived_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, entity_type, key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, block_id) REFERENCES custom_field_blocks(tenant_id, id)
);

CREATE TABLE custom_field_options (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    field_id    UUID NOT NULL,
    label       JSONB NOT NULL,
    color       VARCHAR(7),
    sort_order  INT  NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    FOREIGN KEY (tenant_id, field_id) REFERENCES custom_field_definitions(tenant_id, id)
);

CREATE TABLE saved_views (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    user_id     UUID, -- NULL = общий вид организации
    entity_type VARCHAR(50) NOT NULL,
    name        VARCHAR(100) NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}',
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);

-- ---------------------------------------------------------------- люди
CREATE TABLE employees (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    user_id       UUID,
    full_name     VARCHAR(255) NOT NULL,
    phone         VARCHAR(20),
    email         VARCHAR(255),
    position      VARCHAR(100),
    is_teacher    BOOLEAN NOT NULL DEFAULT TRUE,
    subjects      TEXT[] NOT NULL DEFAULT '{}',
    branch_ids    UUID[] NOT NULL DEFAULT '{}',
    hired_at      DATE,
    status        VARCHAR(20) NOT NULL DEFAULT 'active', -- active, archived
    pay_type      VARCHAR(20) NOT NULL DEFAULT 'per_lesson', -- fixed, per_lesson, hourly, percent, mixed
    pay_rate_tiyn BIGINT NOT NULL DEFAULT 0,
    custom_fields JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);

CREATE SEQUENCE students_number_seq START 10001;

CREATE TABLE students (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    number         BIGINT NOT NULL DEFAULT nextval('students_number_seq'),
    branch_id      UUID NOT NULL,
    full_name      VARCHAR(255) NOT NULL,
    birth_date     DATE,
    gender         VARCHAR(10),
    phone          VARCHAR(20),
    email          VARCHAR(255),
    address        VARCHAR(500),
    iin            VARCHAR(12),
    source         VARCHAR(100),
    responsible_id UUID,
    status         VARCHAR(30) NOT NULL DEFAULT 'registration',
    lead_id        UUID,
    comment        TEXT,
    custom_fields  JSONB NOT NULL DEFAULT '{}',
    version        INT NOT NULL DEFAULT 1,
    started_at     DATE,
    archived_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id),
    FOREIGN KEY (tenant_id, responsible_id) REFERENCES users(tenant_id, id)
);
CREATE INDEX students_tenant_name ON students (tenant_id, full_name, id);
CREATE INDEX students_name_trgm ON students USING GIN (full_name gin_trgm_ops);
CREATE INDEX students_phone_trgm ON students USING GIN (phone gin_trgm_ops);
CREATE INDEX students_cf_gin ON students USING GIN (custom_fields jsonb_path_ops);
CREATE INDEX students_iin ON students (tenant_id, iin) WHERE iin IS NOT NULL;

CREATE TABLE parents (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL REFERENCES tenants(id),
    full_name          VARCHAR(255) NOT NULL,
    phone              VARCHAR(20),
    email              VARCHAR(255),
    telegram_chat_id   BIGINT,
    telegram_link_code VARCHAR(32) UNIQUE,
    whatsapp           VARCHAR(20),
    preferred_channel  VARCHAR(20) NOT NULL DEFAULT 'telegram',
    locale             VARCHAR(5) NOT NULL DEFAULT 'ru',
    address            VARCHAR(500),
    consent_at         TIMESTAMPTZ,
    consent_method     VARCHAR(50),
    consent_version    VARCHAR(20),
    comment            TEXT,
    custom_fields      JSONB NOT NULL DEFAULT '{}',
    archived_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);
CREATE INDEX parents_name_trgm ON parents USING GIN (full_name gin_trgm_ops);
CREATE INDEX parents_phone_trgm ON parents USING GIN (phone gin_trgm_ops);

CREATE TABLE student_parents (
    tenant_id    UUID NOT NULL,
    student_id   UUID NOT NULL,
    parent_id    UUID NOT NULL,
    relation     VARCHAR(30) NOT NULL DEFAULT 'parent', -- mother, father, guardian, other
    is_payer     BOOLEAN NOT NULL DEFAULT FALSE,
    is_legal_rep BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (student_id, parent_id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id),
    FOREIGN KEY (tenant_id, parent_id) REFERENCES parents(tenant_id, id)
);

CREATE TABLE student_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL,
    student_id UUID NOT NULL,
    author_id  UUID,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id)
);

-- ---------------------------------------------------------------- обучение
CREATE TABLE courses (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id),
    name             VARCHAR(255) NOT NULL,
    description      TEXT,
    category         VARCHAR(100),
    duration_minutes INT NOT NULL DEFAULT 90,
    lessons_count    INT NOT NULL DEFAULT 12,
    price_tiyn       BIGINT NOT NULL DEFAULT 0,
    status           VARCHAR(20) NOT NULL DEFAULT 'active',
    custom_fields    JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

CREATE TABLE groups (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    branch_id     UUID NOT NULL,
    course_id     UUID NOT NULL,
    name          VARCHAR(255) NOT NULL,
    type          VARCHAR(20) NOT NULL DEFAULT 'group', -- group, individual
    teacher_id    UUID,
    classroom_id  UUID,
    start_date    DATE,
    end_date      DATE,
    capacity      INT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active', -- active, archived
    custom_fields JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id),
    FOREIGN KEY (tenant_id, course_id) REFERENCES courses(tenant_id, id),
    FOREIGN KEY (tenant_id, teacher_id) REFERENCES employees(tenant_id, id),
    FOREIGN KEY (tenant_id, classroom_id) REFERENCES classrooms(tenant_id, id)
);

CREATE TABLE group_students (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    group_id     UUID NOT NULL,
    student_id   UUID NOT NULL,
    joined_at    DATE NOT NULL DEFAULT CURRENT_DATE,
    left_at      DATE,
    leave_reason VARCHAR(100),
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id)
);
CREATE UNIQUE INDEX group_students_active_uq ON group_students (group_id, student_id) WHERE left_at IS NULL;

CREATE TABLE group_waitlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL,
    group_id   UUID NOT NULL,
    student_id UUID,
    lead_id    UUID,
    priority   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id)
);

CREATE TABLE schedules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    group_id        UUID NOT NULL,
    weekdays        INT[] NOT NULL, -- 1=Пн … 7=Вс
    start_time      TIME NOT NULL,
    end_time        TIME NOT NULL,
    teacher_id      UUID,
    classroom_id    UUID,
    valid_from      DATE NOT NULL,
    valid_to        DATE,
    generated_until DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id),
    FOREIGN KEY (tenant_id, teacher_id) REFERENCES employees(tenant_id, id),
    FOREIGN KEY (tenant_id, classroom_id) REFERENCES classrooms(tenant_id, id)
);

CREATE TABLE lessons (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    branch_id    UUID NOT NULL,
    group_id     UUID NOT NULL,
    schedule_id  UUID,
    teacher_id   UUID,
    classroom_id UUID,
    starts_at    TIMESTAMPTZ NOT NULL,
    ends_at      TIMESTAMPTZ NOT NULL,
    type         VARCHAR(20) NOT NULL DEFAULT 'regular', -- regular, trial, makeup, individual, online
    status       VARCHAR(20) NOT NULL DEFAULT 'planned', -- planned, done, cancelled, needs_decision
    topic        VARCHAR(500),
    meeting_url  VARCHAR(500),
    is_detached  BOOLEAN NOT NULL DEFAULT FALSE, -- изменено отдельно от серии
    closed_at    TIMESTAMPTZ,
    closed_by    UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    CHECK (ends_at > starts_at),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id),
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id),
    FOREIGN KEY (tenant_id, schedule_id) REFERENCES schedules(tenant_id, id),
    FOREIGN KEY (tenant_id, teacher_id) REFERENCES employees(tenant_id, id),
    FOREIGN KEY (tenant_id, classroom_id) REFERENCES classrooms(tenant_id, id)
);
CREATE INDEX lessons_tenant_time ON lessons (tenant_id, starts_at);
CREATE UNIQUE INDEX lessons_schedule_slot ON lessons (schedule_id, starts_at) WHERE schedule_id IS NOT NULL;

-- Участники занятия сверх состава группы: пробные лиды, разовые ученики.
CREATE TABLE lesson_guests (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL,
    lesson_id  UUID NOT NULL,
    student_id UUID,
    lead_id    UUID,
    is_trial   BOOLEAN NOT NULL DEFAULT TRUE,
    FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id)
);

CREATE TABLE attendance (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL,
    lesson_id  UUID NOT NULL,
    student_id UUID,
    lead_id    UUID,
    status     VARCHAR(20) NOT NULL, -- present, late, absent, excused, trial
    reason     VARCHAR(255),
    comment    TEXT,
    marked_by  UUID,
    marked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id)
);
CREATE UNIQUE INDEX attendance_lesson_student ON attendance (lesson_id, student_id) WHERE student_id IS NOT NULL;
CREATE UNIQUE INDEX attendance_lesson_lead ON attendance (lesson_id, lead_id) WHERE lead_id IS NOT NULL;

CREATE TABLE attendance_history (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     UUID NOT NULL,
    attendance_id UUID NOT NULL,
    lesson_id     UUID NOT NULL,
    student_id    UUID,
    old_status    VARCHAR(20),
    new_status    VARCHAR(20) NOT NULL,
    reason        VARCHAR(255),
    changed_by    UUID,
    changed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE attendance_charge_rules (
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    status      VARCHAR(20) NOT NULL,
    charge      BOOLEAN NOT NULL,
    pay_teacher BOOLEAN NOT NULL,
    PRIMARY KEY (tenant_id, status)
);

-- ---------------------------------------------------------------- финансы
CREATE TABLE subscriptions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    student_id    UUID NOT NULL,
    course_id     UUID,
    type          VARCHAR(20) NOT NULL, -- lessons, monthly, period, unlimited, custom
    name          VARCHAR(255) NOT NULL,
    price_tiyn    BIGINT NOT NULL,
    lessons_total INT,
    starts_on     DATE NOT NULL,
    ends_on       DATE,
    status        VARCHAR(20) NOT NULL DEFAULT 'active', -- active, frozen, expired, used, cancelled
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id),
    FOREIGN KEY (tenant_id, course_id) REFERENCES courses(tenant_id, id)
);

-- Списания занятий: +1 списано, -1 корректировка (возврат занятия).
CREATE TABLE subscription_usages (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    subscription_id UUID NOT NULL,
    lesson_id       UUID NOT NULL,
    student_id      UUID NOT NULL,
    qty             INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, subscription_id) REFERENCES subscriptions(tenant_id, id),
    FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id)
);
CREATE INDEX subscription_usages_lesson ON subscription_usages (lesson_id, student_id);

CREATE TABLE subscription_freezes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    subscription_id UUID NOT NULL,
    starts_on       DATE NOT NULL,
    ends_on         DATE NOT NULL,
    reason          VARCHAR(255),
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, subscription_id) REFERENCES subscriptions(tenant_id, id)
);

CREATE TABLE payments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    student_id      UUID NOT NULL,
    payer_parent_id UUID,
    amount_tiyn     BIGINT NOT NULL CHECK (amount_tiyn > 0),
    currency        CHAR(3) NOT NULL DEFAULT 'KZT',
    paid_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    method          VARCHAR(20) NOT NULL, -- cash, card, transfer, kaspi, online, other
    received_by     UUID,
    status          VARCHAR(20) NOT NULL DEFAULT 'completed', -- completed, partially_refunded, refunded
    comment         TEXT,
    external_id     VARCHAR(255),
    receipt_number  VARCHAR(100),
    custom_fields   JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id),
    FOREIGN KEY (tenant_id, payer_parent_id) REFERENCES parents(tenant_id, id)
);
CREATE UNIQUE INDEX payments_external_uq ON payments (tenant_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX payments_tenant_paid ON payments (tenant_id, paid_at);

CREATE TABLE refunds (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    payment_id  UUID NOT NULL,
    amount_tiyn BIGINT NOT NULL CHECK (amount_tiyn > 0),
    reason      VARCHAR(500),
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, payment_id) REFERENCES payments(tenant_id, id)
);

-- Проводки. Баланс = SUM(amount_tiyn): платёж (+), начисление (−), возврат (−), корректировка (±).
CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    student_id  UUID NOT NULL,
    kind        VARCHAR(20) NOT NULL, -- payment, charge, refund, adjustment
    amount_tiyn BIGINT NOT NULL,
    ref_type    VARCHAR(30),
    ref_id      UUID,
    comment     TEXT,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id)
);
CREATE INDEX ledger_student ON ledger_entries (student_id);

CREATE TABLE student_balances (
    tenant_id    UUID NOT NULL,
    student_id   UUID PRIMARY KEY,
    balance_tiyn BIGINT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id)
);

-- ---------------------------------------------------------------- лиды
CREATE TABLE leads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    branch_id       UUID,
    full_name       VARCHAR(255) NOT NULL,
    contact_name    VARCHAR(255),
    phone           VARCHAR(20) NOT NULL,
    email           VARCHAR(255),
    telegram        VARCHAR(100),
    course_id       UUID,
    preferred_time  VARCHAR(255),
    age_or_grade    VARCHAR(50),
    source          VARCHAR(100),
    utm             JSONB NOT NULL DEFAULT '{}',
    responsible_id  UUID,
    stage           VARCHAR(30) NOT NULL DEFAULT 'new',
    lost_reason     VARCHAR(255),
    student_id      UUID,
    last_contact_at TIMESTAMPTZ,
    comment         TEXT,
    custom_fields   JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, branch_id) REFERENCES branches(tenant_id, id),
    FOREIGN KEY (tenant_id, course_id) REFERENCES courses(tenant_id, id),
    FOREIGN KEY (tenant_id, responsible_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id)
);
CREATE INDEX leads_name_trgm ON leads USING GIN (full_name gin_trgm_ops);

-- ---------------------------------------------------------------- инфраструктура
CREATE TABLE audit_logs (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       UUID,
    user_id         UUID,
    entity_type     VARCHAR(50) NOT NULL,
    entity_id       UUID,
    action          VARCHAR(50) NOT NULL,
    old_values      JSONB,
    new_values      JSONB,
    ip_address      VARCHAR(64),
    user_agent      TEXT,
    impersonated_by UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX audit_entity ON audit_logs (tenant_id, entity_type, entity_id, created_at DESC);

CREATE TABLE access_logs (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    user_id     UUID,
    entity_type VARCHAR(50) NOT NULL,
    entity_id   UUID,
    field       VARCHAR(64) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    UUID NOT NULL,
    event_type   VARCHAR(100) NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT
);
CREATE INDEX outbox_pending ON outbox_events (id) WHERE processed_at IS NULL;

CREATE TABLE notification_logs (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  UUID NOT NULL,
    parent_id  UUID,
    channel    VARCHAR(20) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    body       TEXT NOT NULL,
    status     VARCHAR(20) NOT NULL, -- sent, failed, skipped, deferred
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_keys (
    tenant_id  UUID NOT NULL,
    key        VARCHAR(100) NOT NULL,
    response   JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, key)
);

CREATE TABLE export_logs (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    user_id     UUID,
    entity_type VARCHAR(50) NOT NULL,
    rows_count  INT NOT NULL,
    format      VARCHAR(10) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------- RLS
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'branches','classrooms','roles','user_roles','user_branch_access',
        'custom_field_blocks','custom_field_definitions','custom_field_options','saved_views',
        'employees','students','parents','student_parents','student_comments',
        'courses','groups','group_students','group_waitlist','schedules','lessons','lesson_guests',
        'attendance','attendance_history','attendance_charge_rules',
        'subscriptions','subscription_usages','subscription_freezes','payments','refunds',
        'ledger_entries','student_balances','leads','access_logs','outbox_events',
        'notification_logs','idempotency_keys','export_logs'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant())', t);
    END LOOP;
END $$;

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenants USING (id = app_current_tenant()) WITH CHECK (id = app_current_tenant());

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON users USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant());

ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_logs USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant());

-- Праздники: глобальные видны всем, свои — только своему tenant'у; писать можно только свои.
ALTER TABLE calendar_holidays ENABLE ROW LEVEL SECURITY;
CREATE POLICY holidays_read ON calendar_holidays FOR SELECT USING (tenant_id IS NULL OR tenant_id = app_current_tenant());
CREATE POLICY holidays_write ON calendar_holidays FOR ALL USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant());
