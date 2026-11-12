-- Тесты на уроке и КТП: баллы за тесты, тема и ДЗ вводятся при отметке занятия.

-- Тест на уроке: off — только вручную в карточке ученика, optional — можно заполнить при отметке,
-- required — без него занятие не закрывается. course_ids — для каких курсов (пусто — для всех).
ALTER TABLE test_types ADD COLUMN lesson_mode VARCHAR(10) NOT NULL DEFAULT 'optional' CHECK (lesson_mode IN ('off', 'optional', 'required'));
ALTER TABLE test_types ADD COLUMN course_ids UUID[] NOT NULL DEFAULT '{}';
-- тесты, скрытые из Mini App, на урок не выводятся
UPDATE test_types SET lesson_mode = 'off' WHERE NOT show_in_miniapp;
ALTER TABLE test_types DROP COLUMN show_in_miniapp;

ALTER TABLE student_tests ADD COLUMN lesson_id UUID;
ALTER TABLE student_tests ADD CONSTRAINT student_tests_lesson_fk FOREIGN KEY (tenant_id, lesson_id) REFERENCES lessons(tenant_id, id);
-- за урок у ученика один результат каждого теста (повторное сохранение отметки его заменяет)
CREATE UNIQUE INDEX student_tests_lesson_uq ON student_tests (lesson_id, student_id, test_type_id) WHERE lesson_id IS NOT NULL;

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
    FOREACH t IN ARRAY ARRAY['ktp_categories', 'ktp_topics'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant())', t);
    END LOOP;
END $$;
