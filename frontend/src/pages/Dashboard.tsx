import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Circle } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useLang, tr, intlLocale } from '../lib/i18n'
import { dateShort, longDay, money, plural, time } from '../lib/format'
import type { Lesson } from '../lib/types'
import { Banner, Card, Segmented, Spinner, Stat, StatusBadge, Table, lessonState, cx } from '../components/ui'

type Dash = {
  greeting_name: string
  period: { name: string }
  active_students?: { value: number; delta: number }
  new_leads?: { value: number; won: number }
  attendance_rate?: number | null
  income?: { value: number; previous: number }
  debt?: { value: number; debtors: number }
  weekly_income?: { week: string; amount_tiyn: number; partial: boolean }[]
  lessons_today?: Lesson[]
  attention: Record<string, number>
  plan_limit?: { max: number; active: number; since: string | null }
}

const periodLabel: Record<string, string> = { month: tr('к прошлому месяцу'), week: tr('к прошлой неделе'), today: tr('ко вчера') }

export default function Dashboard() {
  const { branchId, can, me, isTeacherOnly } = useAuth()
  const [, t] = useLang()
  const [period, setPeriod] = useState<'month' | 'week' | 'today'>('month')
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['dashboard', branchId, period], queryFn: () => api.get<Dash>(`/dashboard?period=${period}&branch_id=${branchId}`), refetchInterval: 60_000 })
  const org = useQuery({ queryKey: ['organization'], queryFn: () => api.get('/organization'), enabled: can('settings.manage') })
  if (q.isLoading || !q.data) return <Spinner />
  const d = q.data
  const branchName = me?.branches?.find((b) => b.id === branchId)?.name ?? tr('все филиалы')
  const hour = new Date().getHours()
  const greet = hour < 12 ? tr('Доброе утро') : hour < 18 ? t('dash.hello', 'Добрый день') : tr('Добрый вечер')
  const incomeDelta = d.income && d.income.previous > 0 ? Math.round(((d.income.value - d.income.previous) / d.income.previous) * 100) : null
  const onboarding = org.data?.onboarding as Record<string, boolean> | undefined
  const onboardingLeft = onboarding && Object.values(onboarding).some((v) => !v)

  return (
    <div className="mx-auto max-w-[1400px] space-y-6 p-4 lg:p-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-[26px] font-bold">{greet}, {d.greeting_name}</h1>
          <p className="mt-1 text-sm text-muted first-letter:uppercase">{longDay(new Date())} · {branchName}</p>
        </div>
        {!isTeacherOnly && <Segmented value={period} onChange={setPeriod} items={[{ value: 'month', label: tr('Месяц') }, { value: 'week', label: tr('Неделя') }, { value: 'today', label: tr('Сегодня') }]} />}
      </div>

      {d.plan_limit && (
        <Banner tone="orange">{tr('Активных учеников')} {d.plan_limit.active} {tr('при лимите тарифа')} {d.plan_limit.max}{tr('. В течение 14 дней смените тариф — затем станет недоступно только создание новых учеников.')}</Banner>
      )}

      {onboardingLeft && onboarding && (
        <Card title={tr('Первые шаги')} action={<span className="text-sm text-muted">{Object.values(onboarding).filter(Boolean).length} {tr('из')} {Object.keys(onboarding).length}</span>}>
          <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-6">
            {[['branch', tr('Филиал'), '/settings/branches'], ['teacher', tr('Преподаватель'), '/teachers'], ['course', tr('Курс'), '/groups?tab=courses'], ['group', tr('Группа'), '/groups?new=group'], ['students', tr('Импорт учеников'), '/students?import=1'], ['schedule', tr('Расписание'), '/groups']].map(([k, l, to]) => (
              <Link key={k} to={to} className={cx('flex items-center gap-2 rounded-lg border px-3 py-2.5 text-sm', onboarding[k] ? 'border-green-200 bg-green-50 text-green-800' : 'border-line hover:bg-gray-50')}>
                {onboarding[k] ? <CheckCircle2 className="size-4" /> : <Circle className="size-4 text-muted" />}{l}
              </Link>
            ))}
          </div>
        </Card>
      )}

      {!isTeacherOnly && (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
          {d.active_students && <Stat label={tr('Активные ученики')} value={d.active_students.value} sub={<span className="text-green-700">+{d.active_students.delta} {periodLabel[period]}</span>} onClick={() => nav('/students?status=active')} />}
          {d.new_leads && <Stat label={tr('Новые лиды')} value={d.new_leads.value} sub={<span className="text-muted">{tr('конверсия в ученика')} {d.new_leads.value ? Math.round((d.new_leads.won / d.new_leads.value) * 100) : 0}%</span>} onClick={() => nav('/leads')} />}
          {d.attendance_rate !== undefined && <Stat label={tr('Посещаемость')} value={d.attendance_rate != null ? `${d.attendance_rate}%` : '—'} sub={<span className="text-muted">{tr('за последние 30 дней')}</span>} onClick={() => nav('/reports')} />}
          {d.income && <Stat label={tr('Поступления')} value={money(d.income.value)} sub={incomeDelta != null ? <span className={incomeDelta >= 0 ? 'text-green-700' : 'text-red-700'}>{incomeDelta >= 0 ? '+' : ''}{incomeDelta}% {periodLabel[period]}</span> : <span className="text-muted">{tr('за период')}</span>} onClick={() => nav('/finance')} />}
          {d.debt && <Stat tone="warn" label={tr('Задолженность')} value={money(d.debt.value)} sub={<span className="text-orange-800">{d.debt.debtors} {plural(d.debt.debtors, tr('должник'), tr('должника'), tr('должников'))} →</span>} onClick={() => nav('/finance?tab=debtors')} />}
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)] [&>*]:min-w-0">
        <Card title={isTeacherOnly ? tr('Мои занятия сегодня') : tr('Занятия сегодня')} action={<Link to="/schedule" className="text-sm font-medium text-brand-600 hover:underline">{tr('Всё расписание')}</Link>} bodyClass="!px-0 !pb-2">
          {(d.lessons_today?.length ?? 0) > 0 && (
            <ul className="divide-y divide-line border-t border-line md:hidden">
              {d.lessons_today!.map((l) => (
                <li key={l.id}><button onClick={() => nav(`/attendance/${l.id}`)} className="flex w-full items-center gap-3 px-5 py-3 text-left">
                  <span className="w-12 shrink-0 text-sm font-semibold tabular">{time(l.starts_at)}</span>
                  <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{l.group_name}</span><span className="block truncate text-xs text-muted">{l.classroom_name} · {l.students} {tr('уч.')}</span></span>
                  <StatusBadge map={lessonState} value={l.state} />
                </button></li>
              ))}
            </ul>
          )}
          {(d.lessons_today?.length ?? 0) === 0 ? <div className="px-5 py-10 text-center text-sm text-muted">{tr('Сегодня занятий нет')}</div> : (
            <Table className="hidden md:block" head={[tr('Время'), tr('Группа'), tr('Преподаватель'), tr('Кабинет'), tr('Статус')]}>
              {d.lessons_today!.map((l) => (
                <tr key={l.id} className="cursor-pointer hover:bg-gray-50" onClick={() => nav(`/attendance/${l.id}`)}>
                  <td className="whitespace-nowrap px-4 py-3.5 font-medium tabular">{time(l.starts_at)}–{time(l.ends_at)}</td>
                  <td className="px-4 py-3.5">
                    <div className="font-medium">{l.group_name}</div>
                    <div className="text-xs text-muted">{l.students} {plural(l.students, tr('ученик'), tr('ученика'), tr('учеников'))}{l.trials > 0 && tr(' · {0} пробный', l.trials)}</div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3.5">{l.teacher_name}</td>
                  <td className="whitespace-nowrap px-4 py-3.5 text-muted">{l.classroom_name}</td>
                  <td className="px-4 py-3.5"><StatusBadge map={lessonState} value={l.conflict && l.state === 'planned' ? 'planned' : l.state} /></td>
                </tr>
              ))}
            </Table>
          )}
        </Card>

        <div className="space-y-6">
          <Card title={tr('Требует внимания')}>
            <ul className="divide-y divide-line">
              {[
                ['unmarked_lessons', tr('Занятия без отметки посещаемости'), '/attendance', 'text-red-700'],
                ['subscriptions_ending', tr('Абонементы заканчиваются за 7 дней'), '/students?subscription=ending', 'text-red-700'],
                ['leads_no_contact', tr('Лиды без контакта больше 24 ч'), '/leads?no_contact=1', 'text-red-700'],
                ['trials_today', tr('Пробные занятия сегодня'), '/schedule', 'text-purple-700'],
                ['freezes_ending', tr('Заморозки заканчиваются'), '/students?status=frozen', 'text-brand-600'],
                ['holiday_decisions', tr('Занятия в нерабочие дни ждут решения'), '/schedule', 'text-amber-700'],
              ].filter(([k]) => d.attention[k] !== undefined).map(([k, l, to, c]) => (
                <li key={k}><Link to={to} className="flex items-center justify-between py-3 text-sm hover:text-brand-600"><span>{l}</span><span className={cx('font-semibold tabular', c)}>{d.attention[k]}</span></Link></li>
              ))}
            </ul>
          </Card>
          {d.weekly_income && <WeeklyChart data={d.weekly_income} />}
        </div>
      </div>
    </div>
  )
}

function WeeklyChart({ data }: { data: { week: string; amount_tiyn: number; partial: boolean }[] }) {
  const max = Math.max(...data.map((d) => d.amount_tiyn), 1)
  return (
    <Card title={tr('Поступления по неделям')}>
      <div className="-mt-2 mb-4 text-[13px] text-muted">{tr('тыс. ₸, текущая неделя неполная')}</div>
      <div className="flex h-40 items-end gap-3">
        {data.map((d) => {
          const k = Math.round(d.amount_tiyn / 100 / 1000)
          return (
            <div key={d.week} className="flex flex-1 flex-col items-center gap-1.5" title={money(d.amount_tiyn)}>
              <span className="text-xs font-semibold tabular">{k.toLocaleString(intlLocale())}</span>
              <div className={cx('w-full rounded-t-md', d.partial ? 'bg-brand-200' : 'bg-brand-600')} style={{ height: `${Math.max(4, (d.amount_tiyn / max) * 110)}px` }} />
              <span className="text-xs text-muted tabular">{dateShort(d.week + 'T12:00:00+05:00')}</span>
            </div>
          )
        })}
      </div>
    </Card>
  )
}
