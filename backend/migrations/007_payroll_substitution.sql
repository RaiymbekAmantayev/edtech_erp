-- Начисления преподавателям за проведённые занятия и замена преподавателя.

-- Замена: исходный преподаватель занятия, если урок ведёт другой. Начисление получает фактический (lessons.teacher_id).
ALTER TABLE lessons ADD COLUMN substitute_for UUID;
ALTER TABLE lessons ADD CONSTRAINT lessons_substitute_for_fk FOREIGN KEY (tenant_id, substitute_for) REFERENCES employees(tenant_id, id);

-- Начисление за проведённое занятие — единый источник для отчёта по зарплате и кабинета преподавателя.
-- Ставка фиксируется при закрытии урока; правка отметок пересчитывает сумму по той же ставке,
-- новые ставки применяются только явным пересчётом.
CREATE TABLE teacher_accruals (
    lesson_id      UUID PRIMARY KEY,
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    teacher_id     UUID NOT NULL,
    branch_id      UUID NOT NULL,
    group_id       UUID NOT NULL,
    starts_at      TIMESTAMPTZ NOT NULL,
    minutes        INT NOT NULL,
    pay_type       VARCHAR(20) NOT NULL,
    rate_tiyn      BIGINT NOT NULL,
    students_total INT NOT NULL DEFAULT 0, -- отмечено учеников
    students_paid  INT NOT NULL DEFAULT 0, -- со статусом «оплачивается преподавателю» (правила списания)
    present        INT NOT NULL DEFAULT 0,
    late           INT NOT NULL DEFAULT 0,
    absent         INT NOT NULL DEFAULT 0,
    excused        INT NOT NULL DEFAULT 0,
    trial          INT NOT NULL DEFAULT 0,
    revenue_tiyn   BIGINT NOT NULL DEFAULT 0, -- списано с учеников за урок (для оплаты процентом)
    amount_tiyn    BIGINT NOT NULL,
    formula        TEXT NOT NULL,
    substitute_for UUID,
    calculated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, teacher_id) REFERENCES employees(tenant_id, id)
);
CREATE INDEX teacher_accruals_teacher_idx ON teacher_accruals (tenant_id, teacher_id, starts_at);
CREATE INDEX teacher_accruals_time_idx ON teacher_accruals (tenant_id, starts_at);

ALTER TABLE teacher_accruals ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON teacher_accruals USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant());

-- Право на отчёт по зарплате: владельцу, администратору, бухгалтеру.
UPDATE roles SET permissions = array_append(permissions, 'payroll.view')
WHERE is_system AND code IN ('owner', 'org_admin', 'accountant') AND NOT ('payroll.view' = ANY(permissions));
