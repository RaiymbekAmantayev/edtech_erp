// Начисления преподавателям — одна модель для админки и Telegram-кабинета преподавателя.
import { money } from './format'
import { tr } from './i18n'

export const payTypes: Record<string, string> = { fixed: tr('Оклад'), per_lesson: tr('За занятие'), hourly: tr('Почасовая'), percent: tr('Процент'), mixed: tr('Комбинированная') }

// Проценты хранятся как процент × 100 (4000 = 40%).
export const pctText = (p: number) => `${(p / 100).toString().replace('.', ',')}%`

// Ставка по модели оплаты; для комбинированной — ставка за занятие + процент от стоимости урока.
export const rateText = (payType: string, rate: number, pct = 0) =>
  payType === 'percent' ? pctText(rate)
    : payType === 'hourly' ? tr('{0}/ч', money(rate))
      : payType === 'fixed' ? tr('{0}/мес', money(rate))
        : payType === 'mixed' && pct > 0 ? `${money(rate)} + ${pctText(pct)}`
          : money(rate)

export type PayrollLesson = {
  lesson_id: string; starts_at: string; minutes: number; group: string; course: string; branch: string
  teacher_id: string; teacher: string; substitute_for: string | null; topic: string | null
  students_total: number; students_paid: number; present: number; late: number; absent: number; excused: number; trial: number
  pay_type: string; rate_tiyn: number; pay_percent: number; revenue_tiyn: number; amount_tiyn: number; formula: string; calculated_at: string
}
export type PayrollSum = {
  teacher_id: string; teacher: string; pay_type: string; rate_tiyn: number; pay_percent: number; lessons: number; paid_lessons: number; minutes: number; substitutions: number
  students_paid: number; present: number; late: number; absent: number; excused: number; trial: number; amount_tiyn: number; salary_tiyn: number
}
export type Payroll = { from: string; to: string; teachers: PayrollSum[]; lessons: PayrollLesson[]; total: PayrollSum }

// Настройки расчёта школы (Настройки → Зарплата).
export type PayrollSettings = {
  show_to_teachers: boolean
  min_students: number
  below_min: 'zero' | 'min_amount'
  min_amount_tiyn: number
  substitute: 'own' | 'original' | 'fixed' | 'coef'
  substitute_value: number // fixed — тиын, coef — процент (100 = как есть)
}

// Показ начислений в кабинете преподавателя (карточка преподавателя).
export const payrollVisibility: Record<string, string> = { school: tr('Как в настройках школы'), show: tr('Показывать'), hide: tr('Скрыть') }

export const hours = (m: number) => (Math.round((m / 60) * 10) / 10).toString().replace('.', ',')
