// Начисления преподавателям — одна модель для админки и Telegram-кабинета преподавателя.
import { money } from './format'

export const payTypes: Record<string, string> = { fixed: 'Оклад', per_lesson: 'За занятие', hourly: 'Почасовая', percent: 'Процент', mixed: 'Комбинированная' }

// Для оплаты процентом ставка хранится как процент × 100 (4000 = 40%).
export const rateText = (payType: string, rate: number) =>
  payType === 'percent' ? `${(rate / 100).toString().replace('.', ',')}%` : payType === 'hourly' ? `${money(rate)}/ч` : payType === 'fixed' ? `${money(rate)}/мес` : money(rate)

export type PayrollLesson = {
  lesson_id: string; starts_at: string; minutes: number; group: string; course: string; branch: string
  teacher_id: string; teacher: string; substitute_for: string | null; topic: string | null
  students_total: number; students_paid: number; present: number; late: number; absent: number; excused: number; trial: number
  pay_type: string; rate_tiyn: number; revenue_tiyn: number; amount_tiyn: number; formula: string; calculated_at: string
}
export type PayrollSum = {
  teacher_id: string; teacher: string; pay_type: string; rate_tiyn: number; lessons: number; paid_lessons: number; minutes: number; substitutions: number
  students_paid: number; present: number; late: number; absent: number; excused: number; trial: number; amount_tiyn: number; salary_tiyn: number
}
export type Payroll = { from: string; to: string; teachers: PayrollSum[]; lessons: PayrollLesson[]; total: PayrollSum }

export const hours = (m: number) => (Math.round((m / 60) * 10) / 10).toString().replace('.', ',')
