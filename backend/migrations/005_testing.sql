-- Тестирование учеников (как в HolliHope): школа сама настраивает типы тестов, навыки и шкалу оценок.
-- Баллы за урок вводятся при отметке посещаемости; там же тема и ДЗ из КТП школы.

-- Навыки — справочник школы (Grammar, Listening, Математика…), подключаются к типам тестов.
CREATE TABLE test_skills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    name        VARCHAR(120) NOT NULL,
    sort_order  INT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

-- Оценки — шкала школы (A1…C2, «Отлично», 5…2). min_percent — порог для автоматической итоговой оценки по баллам.
CREATE TABLE test_grades (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    name        VARCHAR(60) NOT NULL,
    color       VARCHAR(16),
    min_percent NUMERIC(5,2) CHECK (min_percent IS NULL OR (min_percent >= 0 AND min_percent <= 100)),
    sort_order  INT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

-- Тип теста: набор навыков и способ оценки (баллы по навыку или оценка из шкалы).
-- lesson_mode — тест на уроке: off — только вручную в карточке ученика, optional — можно заполнить при отметке,
-- required — без него занятие не закрывается. course_ids — для каких курсов (пусто — для всех).
CREATE TABLE test_types (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    name            VARCHAR(160) NOT NULL,
    description     TEXT,
    scoring         VARCHAR(10) NOT NULL DEFAULT 'score' CHECK (scoring IN ('score', 'grade')),
    lesson_mode     VARCHAR(10) NOT NULL DEFAULT 'optional' CHECK (lesson_mode IN ('off', 'optional', 'required')),
    course_ids      UUID[] NOT NULL DEFAULT '{}',
    sort_order      INT NOT NULL DEFAULT 0,
    archived_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

CREATE TABLE test_type_skills (
    tenant_id    UUID NOT NULL REFERENCES tenants(id),
    test_type_id UUID NOT NULL,
    skill_id     UUID NOT NULL,
    max_score    NUMERIC(8,2) NOT NULL DEFAULT 100 CHECK (max_score > 0),
    sort_order   INT NOT NULL DEFAULT 0,
    PRIMARY KEY (test_type_id, skill_id),
    FOREIGN KEY (tenant_id, test_type_id) REFERENCES test_types(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, skill_id) REFERENCES test_skills(tenant_id, id)
);

-- Результат теста ученика.
CREATE TABLE student_tests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id),
    student_id   UUID NOT NULL,
    test_type_id UUID NOT NULL,
    group_id     UUID,
    lesson_id    UUID,
    teacher_id   UUID,
    taken_on     DATE NOT NULL,
    total_score  NUMERIC(10,2),
    max_score    NUMERIC(10,2),
    percent      NUMERIC(5,2),
    grade_id     UUID,
    comment      TEXT,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, student_id) REFERENCES students(tenant_id, id),
    FOREIGN KEY (tenant_id, test_type_id) REFERENCES test_types(tenant_id, id),
    FOREIGN KEY (tenant_id, group_id) REFERENCES groups(tenant_id, id),
    FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id),
    FOREIGN KEY (tenant_id, teacher_id) REFERENCES employees(tenant_id, id),
    FOREIGN KEY (tenant_id, grade_id) REFERENCES test_grades(tenant_id, id)
);
-- за урок у ученика один результат каждого теста (повторное сохранение отметки его заменяет)
CREATE UNIQUE INDEX student_tests_lesson_uq ON student_tests (lesson_id, student_id, test_type_id) WHERE lesson_id IS NOT NULL;
CREATE INDEX student_tests_student_idx ON student_tests (tenant_id, student_id, taken_on DESC);
CREATE INDEX student_tests_group_idx ON student_tests (tenant_id, group_id, taken_on DESC);

CREATE TABLE student_test_scores (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    test_id   UUID NOT NULL,
    skill_id  UUID NOT NULL,
    score     NUMERIC(8,2),
    max_score NUMERIC(8,2),
    grade_id  UUID,
    PRIMARY KEY (test_id, skill_id),
    FOREIGN KEY (tenant_id, test_id) REFERENCES student_tests(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, skill_id) REFERENCES test_skills(tenant_id, id),
    FOREIGN KEY (tenant_id, grade_id) REFERENCES test_grades(tenant_id, id)
);

-- КТП (календарно-тематический план): категории школы (по предмету, уровню, классу — как удобно) и темы с ДЗ.
CREATE TABLE ktp_categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    name        VARCHAR(160) NOT NULL,
    course_ids  UUID[] NOT NULL DEFAULT '{}', -- курсы, на занятиях которых предлагаются темы категории
    sort_order  INT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id)
);

CREATE TABLE ktp_topics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    category_id UUID NOT NULL,
    title       VARCHAR(500) NOT NULL,
    homework    TEXT,
    hours       NUMERIC(5,1),
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, category_id) REFERENCES ktp_categories(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX ktp_topics_category_idx ON ktp_topics (category_id, sort_order);

-- Тема из КТП и домашнее задание занятия (ДЗ хранится отдельно и может отличаться от КТП).
ALTER TABLE lessons ADD COLUMN ktp_topic_id UUID;
ALTER TABLE lessons ADD COLUMN homework TEXT;
ALTER TABLE lessons ADD CONSTRAINT lessons_ktp_topic_fk FOREIGN KEY (tenant_id, ktp_topic_id) REFERENCES ktp_topics(tenant_id, id);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['test_skills', 'test_grades', 'test_types', 'test_type_skills', 'student_tests', 'student_test_scores', 'ktp_categories', 'ktp_topics'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant())', t);
    END LOOP;
END $$;

-- Новые права: проведение тестов. Добавляются системным ролям существующих школ.
UPDATE roles SET permissions = array_append(permissions, 'tests.conduct')
WHERE is_system AND code IN ('owner', 'org_admin', 'branch_manager', 'academic', 'teacher') AND NOT ('tests.conduct' = ANY(permissions));
