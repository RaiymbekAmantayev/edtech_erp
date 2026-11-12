// Настройки → Зарплата: правила расчёта начислений преподавателям и показ начислений в их кабинете.
// Оплачиваемые статусы посещения — те же правила, что в «Правила списания» (attendance_charge_rules.pay_teacher).
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { money, parseMoney } from '../../lib/format'
import type { PayrollSettings } from '../../lib/payroll'
import { Button, Card, Checkbox, Field, Input, Select, Spinner, Toast, attendanceStatus } from '../../components/ui'
import { tr } from '../../lib/i18n'

type Rule = { status: string; charge: boolean; pay_teacher: boolean }

const statusName = (s: string) => (s === 'excused' ? tr('Уважительная причина') : attendanceStatus[s]?.[0] ?? s)

export default function PayrollSettingsPage() {
  const qc = useQueryClient()
  const sq = useQuery({ queryKey: ['payroll-settings'], queryFn: () => api.get<PayrollSettings>('/settings/payroll') })
  const rq = useQuery({ queryKey: ['att-rules'], queryFn: () => api.get<Rule[]>('/attendance-rules') })
  const [s, setS] = useState<PayrollSettings | null>(null)
  const [rules, setRules] = useState<Rule[] | null>(null)
  // суммы редактируются в тенге, коэффициент замены — в процентах
  const [minAmount, setMinAmount] = useState('')
  const [subValue, setSubValue] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [toast, setToast] = useState('')

  useEffect(() => {
    if (sq.data && !s) {
      setS(sq.data)
      setMinAmount(sq.data.min_amount_tiyn ? String(sq.data.min_amount_tiyn / 100) : '')
      setSubValue(sq.data.substitute === 'fixed' ? String(sq.data.substitute_value / 100) : String(sq.data.substitute_value || 100))
    }
  }, [sq.data, s])
  useEffect(() => { if (rq.data && !rules) setRules(rq.data) }, [rq.data, rules])

  const save = useMutation({
    mutationFn: async () => {
      if (!s || !rules) return
      const body: PayrollSettings = {
        ...s,
        min_amount_tiyn: s.below_min === 'min_amount' ? parseMoney(minAmount) ?? 0 : 0,
        substitute_value: s.substitute === 'fixed' ? parseMoney(subValue) ?? 0 : s.substitute === 'coef' ? Math.round(Number(subValue.replace(',', '.')) || 0) : 100,
      }
      await api.put('/settings/payroll', body)
      await api.put('/attendance-rules', rules)
    },
    onSuccess: () => {
      setErrors({})
      qc.invalidateQueries({ queryKey: ['payroll-settings'] })
      qc.invalidateQueries({ queryKey: ['att-rules'] })
      setToast(tr('Сохранено. Новые правила применяются к занятиям, закрытым после сохранения; прошлые — кнопкой «Пересчитать» в разделе «Зарплата»'))
    },
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })

  if (!s || !rules) return <Spinner />
  const set = (patch: Partial<PayrollSettings>) => setS({ ...s, ...patch })

  return (
    <div className="max-w-3xl space-y-5">
      <h1 className="text-[26px] font-bold">{tr('Зарплата')}</h1>
      <p className="text-sm text-muted">
        {tr('Правила, по которым считаются начисления преподавателям за проведённые занятия. Модель оплаты и ставка задаются в карточке преподавателя.')}
      </p>

      <Card title={tr('Кабинет преподавателя')}>
        <Checkbox checked={s.show_to_teachers} onChange={(v) => set({ show_to_teachers: v })} label={tr('Показывать преподавателям их начисления в Telegram-кабинете')} />
        <p className="mt-2 text-[13px] text-muted">{tr('Для отдельного преподавателя можно сделать исключение в его карточке: «Начисления в кабинете преподавателя».')}</p>
      </Card>

      <Card title={tr('Какие отметки оплачиваются преподавателю')}>
        <p className="mb-3 text-[13px] text-muted">{tr('Начисление за урок считается по ученикам с этими статусами. Те же правила — в «Правила списания».')}</p>
        <div className="grid gap-2 sm:grid-cols-2">
          {rules.map((r, i) => (
            <Checkbox key={r.status} checked={r.pay_teacher} label={statusName(r.status)}
              onChange={(v) => setRules(rules.map((x, j) => (j === i ? { ...x, pay_teacher: v } : x)))} />
          ))}
        </div>
      </Card>

      <Card title={tr('Минимум учеников на уроке')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Минимум оплачиваемых учеников')} error={errors.min_students} hint={tr('0 — урок оплачивается при любом числе учеников')}>
            <Input type="number" min={0} max={100} value={s.min_students} onChange={(e) => set({ min_students: Math.max(0, Number(e.target.value) || 0) })} />
          </Field>
          <Field label={tr('Если учеников меньше минимума')} error={errors.below_min}>
            <Select value={s.below_min} disabled={s.min_students === 0} onChange={(e) => set({ below_min: e.target.value as PayrollSettings['below_min'] })}>
              <option value="zero">{tr('Урок не оплачивается')}</option>
              <option value="min_amount">{tr('Платить минимальную сумму')}</option>
            </Select>
          </Field>
          {s.min_students > 0 && s.below_min === 'min_amount' && (
            <Field label={tr('Минимальная сумма за урок, ₸')} error={errors.min_amount_tiyn}>
              <Input value={minAmount} onChange={(e) => setMinAmount(e.target.value)} placeholder={tr('например, 2000')} />
            </Field>
          )}
        </div>
      </Card>

      <Card title={tr('Оплата замены')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={tr('Заменяющему преподавателю платить')} error={errors.substitute}>
            <Select value={s.substitute} onChange={(e) => set({ substitute: e.target.value as PayrollSettings['substitute'] })}>
              <option value="own">{tr('По его собственной ставке')}</option>
              <option value="original">{tr('По ставке основного преподавателя')}</option>
              <option value="fixed">{tr('Фиксированную сумму за урок')}</option>
              <option value="coef">{tr('Процент от начисления по своей ставке')}</option>
            </Select>
          </Field>
          {s.substitute === 'fixed' && (
            <Field label={tr('Сумма за урок замены, ₸')} error={errors.substitute_value}>
              <Input value={subValue} onChange={(e) => setSubValue(e.target.value)} placeholder={tr('например, 4000')} />
            </Field>
          )}
          {s.substitute === 'coef' && (
            <Field label={tr('Процент от начисления')} error={errors.substitute_value} hint={tr('100 — как обычно, 120 — на 20% больше, 80 — на 20% меньше')}>
              <Input value={subValue} onChange={(e) => setSubValue(e.target.value)} />
            </Field>
          )}
        </div>
        {s.substitute === 'fixed' && parseMoney(subValue) != null && (
          <p className="mt-2 text-[13px] text-muted">{tr('За каждый урок замены —')} {money(parseMoney(subValue) ?? 0)}{tr(', независимо от модели оплаты преподавателя.')}</p>
        )}
      </Card>

      {errors._ && <div className="text-sm text-red-700">{errors._}</div>}
      <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>{tr('Сохранить')}</Button>
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}
