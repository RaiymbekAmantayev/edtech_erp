-- Настройки расчёта зарплаты и показ начислений преподавателю в кабинете.
-- Настройки школы хранятся в tenants.settings -> 'payroll' (см. payroll.Settings).

-- Комбинированная модель: ставка за занятие (pay_rate_tiyn) + процент от стоимости урока (процент × 100, 1000 = 10%).
ALTER TABLE employees ADD COLUMN pay_percent INT NOT NULL DEFAULT 0;
-- Показ начислений в кабинете преподавателя: NULL — как в настройках школы, true — показывать, false — скрыть.
ALTER TABLE employees ADD COLUMN payroll_visible BOOLEAN;

-- Процент комбинированной модели фиксируется в начислении вместе со ставкой.
ALTER TABLE teacher_accruals ADD COLUMN pay_percent INT NOT NULL DEFAULT 0;
