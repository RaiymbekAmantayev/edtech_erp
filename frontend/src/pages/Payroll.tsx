import { Fragment, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Download, RefreshCw } from 'lucide-react'
import { api, download } from '../lib/api'
import { useAuth } from '../lib/auth'
import { date, money, time } from '../lib/format'
import { hours, payTypes, rateText, type Payroll as Report } from '../lib/payroll'
import type { Employee } from '../lib/types'
import { Badge, Button, Card, Empty, Field, Input, Select, Spinner, Stat, Toast, cx } from '../components/ui'
import { tr } from '../lib/i18n'

const thisMonth = () => new Date().toISOString().slice(0, 7)

// Начисления преподавателям за проведённые занятия: итоги, расчёт по каждому уроку и выгрузка для бухгалтерии.
// Те же цифры преподаватель видит в профиле Telegram-кабинета.
export default function Payroll() {
  const { branchId } = useAuth()
  const qc = useQueryClient()
  const [month, setMonth] = useState(thisMonth())
  const [teacher, setTeacher] = useState('')
  const [open, setOpen] = useState<string | null>(null)
  const [toast, setToast] = useState('')
  const [busy, setBusy] = useState(false)
  const qs = new URLSearchParams({ month, ...(teacher && { teacher_id: teacher }), ...(branchId && { branch_id: branchId }) }).toString()
  const q = useQuery({ queryKey: ['payroll', qs], queryFn: () => api.get<Report>('/payroll?' + qs) })
  const teachers = useQuery({ queryKey: ['employees', 'teachers'], queryFn: () => api.get<Employee[]>('/employees?teachers=1') })
  const recalc = useMutation({
    mutationFn: () => api.post<{ lessons: number }>('/payroll/recalc?' + qs),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['payroll'] }); setToast(tr('Пересчитано занятий: {0}', r.lessons)) },
  })
  const r = q.data
  return (
    <div className="space-y-5 p-4 lg:p-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-bold">{tr('Зарплата преподавателей')}</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted">{tr('Начисление считается за каждое проведённое занятие по отметкам и условиям оплаты из карточки преподавателя. Эти же цифры преподаватель видит в профиле Telegram-кабинета.')}</p>
        </div>
        <div className="flex gap-2">
          <Button onClick={() => { if (confirm(tr('Пересчитать занятия периода по текущим ставкам из карточек преподавателей? Ставки, зафиксированные при закрытии уроков, будут заменены.'))) recalc.mutate() }} loading={recalc.isPending}>
            <RefreshCw className="size-4" />{tr('Пересчитать по текущим ставкам')}
          </Button>
          <Button variant="primary" loading={busy} disabled={!r?.lessons.length} onClick={async () => { setBusy(true); try { await download('/payroll/export?' + qs) } finally { setBusy(false) } }}>
            <Download className="size-4" />{tr('Выгрузить в Excel')}
          </Button>
        </div>
      </div>

      <Card>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label={tr('Месяц')}><Input type="month" value={month} onChange={(e) => setMonth(e.target.value || thisMonth())} /></Field>
          <Field label={tr('Преподаватель')}>
            <Select value={teacher} onChange={(e) => setTeacher(e.target.value)}>
              <option value="">{tr('Все преподаватели')}</option>
              {teachers.data?.map((t) => <option key={t.id} value={t.id}>{t.full_name}</option>)}
            </Select>
          </Field>
        </div>
      </Card>

      {!r ? <Spinner /> : (
        <>
          <div className="grid gap-3 sm:grid-cols-4">
            <Stat label={tr('Проведено занятий')} value={r.total.lessons} sub={r.total.substitutions ? tr('из них замен: {0}', r.total.substitutions) : undefined} />
            <Stat label={tr('Часов')} value={hours(r.total.minutes)} />
            <Stat label={tr('Посещений (был + опоздал)')} value={r.total.present + r.total.late} sub={tr('не был: {0} · уваж.: {1}', r.total.absent, r.total.excused)} />
            <Stat label={tr('Начислено за занятия')} value={money(r.total.amount_tiyn)} sub={r.total.salary_tiyn ? tr('+ оклады {0}', money(r.total.salary_tiyn)) : undefined} />
          </div>

          <Card bodyClass="!p-0">
            {r.teachers.length === 0 ? <Empty title={tr('Проведённых занятий нет')} text={tr('Начисления появляются, когда преподаватель закрывает занятие с отметкой посещаемости.')} /> : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                    {['', tr('Преподаватель'), tr('Условия оплаты'), tr('Занятий'), tr('Часов'), tr('Был / опозд. / не был / уваж.'), tr('Начислено')].map((h, i) => <th key={i} className={cx('whitespace-nowrap px-4 py-3', i >= 3 && 'text-right')}>{h}</th>)}
                  </tr></thead>
                  <tbody className="divide-y divide-line">
                    {r.teachers.map((t) => (
                      <Fragment key={t.teacher_id}>
                        <tr className="cursor-pointer hover:bg-gray-50" onClick={() => setOpen(open === t.teacher_id ? null : t.teacher_id)}>
                          <td className="w-8 pl-4 text-muted">{open === t.teacher_id ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}</td>
                          <td className="px-4 py-3 font-medium">{t.teacher}</td>
                          <td className="px-4 py-3 text-[13px]">{payTypes[t.pay_type]} · {rateText(t.pay_type, t.rate_tiyn, t.pay_percent)}</td>
                          <td className="px-4 py-3 text-right tabular">{t.lessons}{t.paid_lessons !== t.lessons && <span className="text-muted"> ({t.paid_lessons} {tr('опл.)')}</span>}{t.substitutions > 0 && <div className="text-[11px] text-muted">{tr('замен:')} {t.substitutions}</div>}</td>
                          <td className="px-4 py-3 text-right tabular">{hours(t.minutes)}</td>
                          <td className="px-4 py-3 text-right tabular">{t.present} / {t.late} / {t.absent} / {t.excused}</td>
                          <td className="px-4 py-3 text-right font-semibold tabular">{money(t.amount_tiyn)}{t.salary_tiyn > 0 && <div className="text-[11px] font-normal text-muted">{tr('+ оклад')} {money(t.salary_tiyn)}</div>}</td>
                        </tr>
                        {open === t.teacher_id && (
                          <tr><td colSpan={7} className="bg-gray-50 px-4 py-3"><LessonsTable rows={r.lessons.filter((l) => l.teacher_id === t.teacher_id)} /></td></tr>
                        )}
                      </Fragment>
                    ))}
                    <tr className="bg-gray-50 font-semibold">
                      <td /><td className="px-4 py-3">{tr('Итого')}</td><td />
                      <td className="px-4 py-3 text-right tabular">{r.total.lessons}</td>
                      <td className="px-4 py-3 text-right tabular">{hours(r.total.minutes)}</td>
                      <td className="px-4 py-3 text-right tabular">{r.total.present} / {r.total.late} / {r.total.absent} / {r.total.excused}</td>
                      <td className="px-4 py-3 text-right tabular">{money(r.total.amount_tiyn)}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

function LessonsTable({ rows }: { rows: Report['lessons'] }) {
  return (
    <table className="w-full rounded-lg bg-white text-[13px]">
      <thead><tr className="border-b border-line text-left text-[11px] uppercase tracking-wide text-muted">
        {[tr('Дата'), tr('Группа'), tr('Тема'), tr('Отмечено'), tr('Расчёт'), tr('Сумма')].map((h) => <th key={h} className="px-3 py-2 font-semibold">{h}</th>)}
      </tr></thead>
      <tbody className="divide-y divide-line">
        {rows.map((l) => (
          <tr key={l.lesson_id} className="align-top">
            <td className="whitespace-nowrap px-3 py-2 tabular">{date(l.starts_at)}<div className="text-muted">{time(l.starts_at)} · {l.minutes} {tr('мин')}</div></td>
            <td className="px-3 py-2">{l.group}{l.substitute_for && <div><Badge tone="purple">{tr('замена за')} {l.substitute_for}</Badge></div>}</td>
            <td className="px-3 py-2 text-gray-700">{l.topic ?? '—'}</td>
            <td className="whitespace-nowrap px-3 py-2 tabular">{l.students_total} {tr('уч.')}<div className="text-muted">{tr('Б')} {l.present} {tr('· О')} {l.late} {tr('· Н')} {l.absent} {tr('· У')} {l.excused}{l.trial ? tr(' · Пр {0}', l.trial) : ''}</div></td>
            <td className="px-3 py-2 text-gray-700">{l.formula}</td>
            <td className="whitespace-nowrap px-3 py-2 text-right font-semibold tabular">{money(l.amount_tiyn)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

