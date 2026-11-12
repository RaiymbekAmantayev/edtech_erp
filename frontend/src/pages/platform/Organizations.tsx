import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Clock, Search } from 'lucide-react'
import { api } from '../../lib/api'
import { date, money, relative } from '../../lib/format'
import { Badge, Banner, Select, Spinner, Stat, cx, type Tone } from '../../components/ui'
import { tr, intlLocale } from '../../lib/i18n'

export type Org = {
  id: string; name: string; city: string | null; center_type: string; branches: number; created_at: string; plan: string; plan_name: string; price_tiyn: number
  status: string; active_students: number; max_students: number | null; last_activity: string | null; activity: number[]; health: string; health_reason: string
  paid_until: string | null; trial_ends_at: string | null; over_limit_since: string | null; marked_rate: number | null; telegram_rate: number | null
}
export const orgStatus: Record<string, [string, Tone]> = { active: [tr('Активна'), 'green'], trial: ['Trial', 'blue'], past_due: [tr('Просрочка'), 'orange'], over_limit: [tr('Сверх лимита'), 'yellow'], suspended: [tr('Приостановлена'), 'gray'] }
export const healthText: Record<string, [string, string]> = { good: [tr('Хорошо'), 'text-green-700'], attention: [tr('Внимание'), 'text-amber-700'], risk: [tr('Риск'), 'text-red-700'] }
const monthYear = (d: string) => new Date(d).toLocaleDateString(intlLocale(), { month: '2-digit', year: 'numeric' })

export function Sparkline({ data }: { data: number[] }) {
  const max = Math.max(...data, 1)
  return (
    <div className="flex h-6 items-end gap-[2px]">
      {data.map((v, i) => <div key={i} className={cx('w-1.5 rounded-sm', v ? 'bg-teal-500/70' : 'bg-gray-200')} style={{ height: v ? `${Math.max(15, (v / max) * 100)}%` : '2px' }} />)}
    </div>
  )
}

export default function Organizations() {
  const nav = useNavigate()
  const [status, setStatus] = useState('')
  const [plan, setPlan] = useState('')
  const [health, setHealth] = useState('')
  const [q, setQ] = useState('')
  const overview = useQuery({ queryKey: ['platform-overview'], queryFn: () => api.get<any>('/platform/overview') })
  const list = useQuery({ queryKey: ['platform-orgs', status, plan, health, q], queryFn: () => api.getMeta<Org[]>(`/platform/organizations?status=${status}&plan=${plan}&health=${health}&q=${encodeURIComponent(q)}`) })
  const counts = (list.data?.meta.counts ?? {}) as Record<string, number>
  const o = overview.data
  return (
    <div className="mx-auto max-w-[1450px] space-y-5 p-4 lg:p-8">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="flex-1 text-[26px] font-bold">{tr('Организации')}</h1>
        <label className="flex h-10 w-80 items-center gap-2 rounded-lg border border-line bg-white px-3"><Search className="size-4 text-muted" /><input className="flex-1 text-sm outline-none" placeholder={tr('Название, БИН, email владельца')} value={q} onChange={(e) => setQ(e.target.value)} /></label>
      </div>
      {o && (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
          <Stat label={tr('Платящие организации')} value={o.paying} sub={<span className="text-muted">+{o.new_paying} {tr('за месяц')}</span>} />
          <Stat label={tr('На пробном периоде')} value={o.trial} sub={<span className="text-muted">{tr('конверсия trial → оплата')} {o.trial_conversion != null ? Math.round(o.trial_conversion) : 0}%</span>} />
          <Stat label="MRR" value={money(o.mrr_tiyn)} sub={<span className="text-muted">{tr('ежемесячная выручка платформы')}</span>} />
          <Stat label={tr('Просрочка оплаты')} value={<span className="text-orange-800">{o.past_due}</span>} sub={<span className="text-muted">{tr('на')} {money(o.past_due_tiyn)} {tr('/ мес')}</span>} />
          <Stat label={tr('Риск оттока')} value={<span className="text-red-800">{o.risk}</span>} sub={<span className="text-muted">{tr('низкая активность 7+ дней')}</span>} />
        </div>
      )}
      {o && (o.over_limit > 0 || o.trial_ending_week > 0) && (
        <div className="grid gap-3 lg:grid-cols-2">
          {o.over_limit > 0 && <Banner tone="orange" icon={<AlertTriangle className="size-4" />}><b>{o.over_limit} {o.over_limit === 1 ? tr('организация превысила') : tr('организации превысили')}</b> {tr('лимит учеников — у них 14 дней на смену тарифа.')}</Banner>}
          {o.trial_ending_week > 0 && <Banner tone="teal" icon={<Clock className="size-4" />}><b>{o.trial_ending_week} trial</b> {tr('заканчиваются на этой неделе —')} {o.trial_ending_no_import} {tr('из них ещё не импортировали учеников.')}</Banner>}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <div className="inline-flex flex-wrap rounded-xl bg-gray-100 p-1">
          {[['', tr('Все'), counts.all], ['active', tr('Активные'), counts.active], ['trial', 'Trial', counts.trial], ['past_due', tr('Просрочка'), counts.past_due], ['over_limit', tr('Сверх лимита'), counts.over_limit], ['suspended', tr('Приостановлены'), counts.suspended]].map(([k, l, n]) => (
            <button key={k as string} onClick={() => setStatus(k as string)} className={cx('rounded-lg px-3.5 py-1.5 text-sm font-medium', status === k ? 'bg-white shadow-sm' : 'text-muted')}>{l} <span className="text-xs">{n ?? 0}</span></button>
          ))}
        </div>
        <div className="flex-1" />
        <Select className="!h-9 w-40" value={plan} onChange={(e) => setPlan(e.target.value)}><option value="">{tr('Все тарифы')}</option>{['starter', 'basic', 'business', 'pro', 'enterprise'].map((p) => <option key={p} value={p}>{p[0].toUpperCase() + p.slice(1)}</option>)}</Select>
        <Select className="!h-9 w-44" value={health} onChange={(e) => setHealth(e.target.value)}><option value="">{tr('Любое здоровье')}</option><option value="good">{tr('Хорошо')}</option><option value="attention">{tr('Внимание')}</option><option value="risk">{tr('Риск')}</option></Select>
      </div>
      <div className="overflow-x-auto rounded-xl border border-line bg-white">
        {list.isLoading ? <Spinner /> : (
          <table className="w-full text-sm">
            <thead><tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
              {[tr('Организация'), tr('Тариф'), tr('Статус'), tr('Активные ученики'), tr('Активность'), tr('Здоровье'), tr('Оплачено до')].map((h, i) => <th key={h} className={cx('px-4 py-3', i === 6 && 'text-right')}>{h}</th>)}
            </tr></thead>
            <tbody className="divide-y divide-line">
              {list.data?.data.map((g) => {
                const pct = g.max_students ? Math.min(100, (g.active_students / g.max_students) * 100) : 0
                const over = !!g.max_students && g.active_students > g.max_students
                return (
                  <tr key={g.id} className="cursor-pointer hover:bg-gray-50" onClick={() => nav('/platform/organizations/' + g.id)}>
                    <td className="px-4 py-3"><div className="font-semibold">{g.name}</div><div className="text-xs text-muted">{g.city} · {g.branches} {g.branches === 1 ? tr('филиал') : tr('филиала')} {tr('· с')} {monthYear(g.created_at)}</div></td>
                    <td className="px-4 py-3"><div className="font-medium">{g.plan_name}</div><div className="text-xs text-muted">{g.price_tiyn ? money(g.price_tiyn) + tr('/мес') : tr('договор')}</div></td>
                    <td className="px-4 py-3"><Badge tone={orgStatus[g.status]?.[1]}>{orgStatus[g.status]?.[0] ?? g.status}</Badge></td>
                    <td className="min-w-44 px-4 py-3">
                      <div className="flex justify-between"><span className={cx('font-semibold tabular', over && 'text-orange-700')}>{g.active_students.toLocaleString(intlLocale())}</span><span className="text-xs text-muted">{tr('из')} {g.max_students?.toLocaleString(intlLocale()) ?? '∞'}</span></div>
                      <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-gray-200"><div className={cx('h-full', over ? 'bg-orange-500' : 'bg-teal-600')} style={{ width: `${pct}%` }} /></div>
                    </td>
                    <td className="px-4 py-3"><Sparkline data={g.activity} /><div className="mt-1 text-xs text-muted">{relative(g.last_activity)}</div></td>
                    <td className="px-4 py-3"><div className={cx('font-semibold', healthText[g.health][1])}>● {healthText[g.health][0]}</div><div className="text-xs text-muted">{g.health_reason}</div></td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      {g.status === 'trial' ? <span>{tr('trial до')} {date(g.trial_ends_at).slice(0, 5)}</span>
                        : g.status === 'past_due' && g.paid_until ? <span className="text-red-700">{tr('просрочено')} {Math.max(0, Math.round((Date.now() - new Date(g.paid_until).getTime()) / 864e5))} {tr('дн.')}</span>
                        : date(g.paid_until)}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
