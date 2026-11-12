import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, AlertCircle } from 'lucide-react'
import { api } from '../../lib/api'
import { date, dateTime, money, relative } from '../../lib/format'
import { Badge, Button, Card, Field, Input, Modal, Select, Spinner, Tabs, cx } from '../../components/ui'
import { healthText, orgStatus, type Org } from './Organizations'
import { tr } from '../../lib/i18n'

type Detail = {
  org: Org; bin: string | null; billing_cycle: string
  limits: { students: number; max_students: number | null; branches: number; max_branches: number | null; staff: number; max_staff: number | null; custom_fields: number; max_custom_fields: number; sms: number }
  activity: { day: string; active_staff: number; marked_rate: number | null }[]
  adoption: { branches: number; courses: number; students: number; groups: number; schedules: number; marked_rate: number | null; payments: number; telegram_rate: number | null; custom_fields: number }
  contacts: { full_name: string; email: string; phone: string; role: string; last_login_at: string | null; mfa: boolean }[]
  integrations: { telegram_sent_7d: number; telegram_failed_24h: number }
  billing: { total_tiyn: number; count: number }
  journal: { action: string; entity_type: string; created_at: string; user: string }[]
}

const actions: Record<string, string> = { registered: tr('Регистрация организации'), plan_changed: tr('Смена тарифа'), subscription_extended: tr('Подписка продлена вручную'), status_changed: tr('Смена статуса'), create: tr('Создание'), update: tr('Изменение'), roles_changed: tr('Изменение ролей'), archive: tr('Архивация') }

export default function OrganizationCard() {
  const { id } = useParams()
  const qc = useQueryClient()
  const [tab, setTab] = useState<'overview' | 'billing' | 'journal'>('overview')
  const [modal, setModal] = useState<null | 'plan' | 'extend'>(null)
  const q = useQuery({ queryKey: ['platform-org', id], queryFn: () => api.get<Detail>('/platform/organizations/' + id) })
  const refresh = () => { qc.invalidateQueries({ queryKey: ['platform-org', id] }); qc.invalidateQueries({ queryKey: ['platform-orgs'] }); qc.invalidateQueries({ queryKey: ['platform-overview'] }) }
  const status = useMutation({ mutationFn: (s: string) => api.post(`/platform/organizations/${id}/status`, { status: s }), onSuccess: refresh })
  if (!q.data) return <Spinner />
  const d = q.data
  const o = d.org
  const forecast = d.limits.max_students ? d.limits.max_students - d.limits.students : null
  return (
    <div className="mx-auto max-w-[1350px] p-4 lg:p-8">
      <div className="text-[13px] text-muted"><Link to="/platform" className="text-teal-700 hover:underline">{tr('Организации')}</Link> / {o.name}</div>
      <div className="mt-3 flex flex-wrap items-start gap-4">
        <div className="flex size-14 items-center justify-center rounded-xl bg-teal-100 text-lg font-bold text-teal-800">{o.name.split(' ').map((p) => p[0]).slice(0, 2).join('')}</div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2"><h1 className="text-[26px] font-bold">{o.name}</h1><Badge tone={orgStatus[o.status]?.[1]}>{orgStatus[o.status]?.[0]}</Badge>
            <Badge tone={o.health === 'good' ? 'green' : o.health === 'risk' ? 'red' : 'yellow'}>{tr('Здоровье:')} {healthText[o.health][0].toLowerCase()}</Badge></div>
          <div className="mt-1 text-sm text-muted">{o.city}{d.bin && tr(' · БИН {0}', d.bin)} {tr('· клиент с')} {date(o.created_at)} · tenant {o.id.slice(0, 4)}…{o.id.slice(-4)}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button disabled title={tr('Вход от имени организации — только с разрешения владельца, с ограничением по времени (в разработке)')}>{tr('Запросить доступ поддержки')}</Button>
          <Button onClick={() => setModal('plan')}>{tr('Сменить тариф')}</Button>
          <Button variant="platform" onClick={() => setModal('extend')}>{tr('Продлить вручную')}</Button>
          {o.status === 'suspended' ? <Button onClick={() => status.mutate('active')}>{tr('Активировать')}</Button>
            : <Button variant="danger" onClick={() => confirm(tr('Приостановить организацию? Данные сохранятся, пользователи выйдут из системы.')) && status.mutate('suspended')}>{tr('Приостановить')}</Button>}
        </div>
      </div>
      <Tabs className="mt-6" value={tab} onChange={setTab} items={[{ value: 'overview', label: tr('Обзор') }, { value: 'billing', label: tr('Подписка и счета') }, { value: 'journal', label: tr('Журнал') }]} />

      {tab === 'overview' && (
        <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
          <div className="space-y-5">
            <Card title={tr('Использование лимитов')} action={<span className="text-xs text-muted">{tr('тариф')} {o.plan_name}</span>}>
              <div className="grid gap-x-8 gap-y-4 sm:grid-cols-2">
                <Limit l={tr('Активные ученики')} v={d.limits.students} m={d.limits.max_students} />
                <Limit l={tr('Филиалы')} v={d.limits.branches} m={d.limits.max_branches} />
                <Limit l={tr('Сотрудники')} v={d.limits.staff} m={d.limits.max_staff} />
                <Limit l={tr('Пользовательские поля')} v={d.limits.custom_fields} m={d.limits.max_custom_fields} />
              </div>
            </Card>
            <Card title={tr('Активность за 30 дней')} action={<span className="flex gap-3 text-xs text-muted"><span><span className="text-teal-600">■</span> {tr('активные сотрудники')}</span><span><span className="text-orange-500">●</span> {tr('% занятий с отметкой')}</span></span>}>
              <ActivityChart data={d.activity} />
            </Card>
            <Card title={tr('Внедрение')}>
              <div className="grid gap-x-8 gap-y-3 sm:grid-cols-2">
                <Step ok={d.adoption.branches > 0} l={tr('Филиалы и кабинеты')} v={`${d.adoption.branches}`} />
                <Step ok={d.adoption.courses > 0} l={tr('Курсы и цены')} v={tr('{0} курсов', d.adoption.courses)} />
                <Step ok={d.adoption.students > 0} l={tr('Импорт учеников')} v={tr('{0} записей', d.adoption.students)} />
                <Step ok={d.adoption.schedules > 0} l={tr('Расписание')} v={tr('{0} групп', d.adoption.groups)} />
                <Step ok={(d.adoption.marked_rate ?? 0) >= 80} l={tr('Посещаемость')} v={d.adoption.marked_rate != null ? tr('{0}% занятий', d.adoption.marked_rate) : tr('нет занятий')} />
                <Step ok={d.adoption.payments > 0} l={tr('Приём оплат')} v={tr('{0} платежей', d.adoption.payments)} />
                <Step ok={(d.adoption.telegram_rate ?? 0) >= 50} l={tr('Telegram-бот родителей')} v={tr('{0}% родителей', d.adoption.telegram_rate ?? 0)} />
                <Step ok={d.adoption.custom_fields > 0} l={tr('Пользовательские поля')} v={tr('{0} полей', d.adoption.custom_fields)} />
              </div>
            </Card>
          </div>
          <div className="space-y-5">
            <Card title={tr('Подписка')}>
              <dl className="space-y-2 text-sm">
                <Row k={tr('Тариф')} v={<b>{o.plan_name} · {d.billing_cycle === 'annual' ? tr('годовая') : tr('помесячно')}</b>} />
                <Row k={tr('Стоимость')} v={o.price_tiyn ? money(o.price_tiyn) + tr(' / мес') : tr('по договору')} />
                <Row k={o.status === 'trial' ? tr('Trial до') : tr('Оплачено до')} v={date(o.status === 'trial' ? o.trial_ends_at : o.paid_until)} />
                <Row k={tr('Всего оплачено')} v={tr('{0} · {1} платежей', money(d.billing.total_tiyn), d.billing.count)} />
              </dl>
              {forecast != null && forecast >= 0 && forecast < 100 && <div className="mt-3 rounded-lg bg-teal-50 p-3 text-[13px] text-teal-900">{tr('До лимита тарифа осталось')} {forecast} {tr('учеников. Предложить тариф выше.')}</div>}
              {forecast != null && forecast < 0 && <div className="mt-3 rounded-lg bg-orange-50 p-3 text-[13px] text-orange-900">{tr('Лимит превышен на')} {-forecast} {tr('учеников')}{o.over_limit_since && tr(' с {0}', date(o.over_limit_since))}.</div>}
            </Card>
            <Card title={tr('Владелец и контакты')}>
              {d.contacts.map((c) => <div key={c.email} className="py-1.5 text-sm"><div className="font-medium">{c.full_name} · {c.role}</div><div className="text-xs text-muted">{c.email}{c.phone && ` · ${c.phone}`} {tr('· вход')} {relative(c.last_login_at)}{c.mfa && ' · MFA'}</div></div>)}
            </Card>
            <Card title={tr('Интеграции')}>
              <div className="space-y-2 text-sm">
                <div className="flex justify-between"><span><span className={d.integrations.telegram_failed_24h ? 'text-red-600' : 'text-green-600'}>●</span> {tr('Telegram-бот')}</span><span className="text-muted">{d.integrations.telegram_sent_7d} {tr('сообщений за 7 дней')}{d.integrations.telegram_failed_24h ? tr(' · {0} ошибок', d.integrations.telegram_failed_24h) : ''}</span></div>
                <div className="flex justify-between"><span><span className="text-gray-400">●</span> amoCRM, SMS, Webhooks</span><span className="text-muted">{tr('этап 7')}</span></div>
              </div>
            </Card>
            <Card title={tr('Журнал платформы')}><Journal items={d.journal.slice(0, 5)} /></Card>
          </div>
        </div>
      )}
      {tab === 'billing' && <Card className="mt-6"><dl className="space-y-2 text-sm"><Row k={tr('Всего оплачено')} v={money(d.billing.total_tiyn)} /><Row k={tr('Платежей')} v={d.billing.count} /><Row k={tr('Оплачено до')} v={date(o.paid_until)} /></dl></Card>}
      {tab === 'journal' && <Card className="mt-6"><Journal items={d.journal} /></Card>}
      {modal === 'plan' && <PlanModal org={o} onClose={() => { setModal(null); refresh() }} />}
      {modal === 'extend' && <ExtendModal org={o} onClose={() => { setModal(null); refresh() }} />}
    </div>
  )
}

const Row = ({ k, v }: { k: string; v: React.ReactNode }) => <div className="flex justify-between gap-3"><dt className="text-muted">{k}</dt><dd className="text-right">{v}</dd></div>
const Journal = ({ items }: { items: Detail['journal'] }) => (
  <ul className="divide-y divide-line">{items.map((j, i) => <li key={i} className="py-2.5 text-sm"><div>{actions[j.action] ?? j.action} · {j.entity_type}</div><div className="text-xs text-muted">{j.user} · {dateTime(j.created_at)}</div></li>)}</ul>
)

function Limit({ l, v, m }: { l: string; v: number; m: number | null }) {
  const pct = m ? Math.min(100, (v / m) * 100) : 0
  return (
    <div>
      <div className="flex justify-between text-sm"><span>{l}</span><span className={cx('font-semibold tabular', !!m && v / m >= 0.8 && 'text-orange-700')}>{v} / {m ?? '∞'}</span></div>
      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-gray-200"><div className={cx('h-full', !!m && v / m >= 0.8 ? 'bg-orange-500' : 'bg-teal-600')} style={{ width: `${pct}%` }} /></div>
    </div>
  )
}

const Step = ({ ok, l, v }: { ok: boolean; l: string; v: string }) => (
  <div className="flex items-center gap-2.5 text-sm">
    <span className={cx('flex size-6 items-center justify-center rounded-full', ok ? 'bg-green-100 text-green-700' : 'bg-amber-100 text-amber-700')}>{ok ? <Check className="size-3.5" /> : <AlertCircle className="size-3.5" />}</span>
    <span className="flex-1">{l}</span><span className="text-xs text-muted">{v}</span>
  </div>
)

function ActivityChart({ data }: { data: Detail['activity'] }) {
  const max = Math.max(...data.map((d) => d.active_staff), 1)
  return (
    <div>
      <div className="relative flex h-32 items-end gap-1">
        {data.map((d) => (
          <div key={d.day} className="relative flex h-full flex-1 items-end" title={tr('{0}: {1} сотр., {2}% отмечено', d.day, d.active_staff, d.marked_rate ?? '—')}>
            <div className="w-full rounded-t-sm bg-teal-500/70" style={{ height: `${(d.active_staff / max) * 75}%` }} />
            {d.marked_rate != null && <span className="absolute left-1/2 size-1.5 -translate-x-1/2 rounded-full bg-orange-500" style={{ bottom: `${d.marked_rate}%` }} />}
          </div>
        ))}
      </div>
      <div className="mt-1.5 flex justify-between text-xs text-muted"><span>{data[0]?.day.slice(8)}.{data[0]?.day.slice(5, 7)}</span><span>{data.at(-1)?.day.slice(8)}.{data.at(-1)?.day.slice(5, 7)}</span></div>
    </div>
  )
}

function PlanModal({ org, onClose }: { org: Org; onClose: () => void }) {
  const [plan, setPlan] = useState(org.plan)
  const m = useMutation({ mutationFn: () => api.post(`/platform/organizations/${org.id}/plan`, { plan }), onSuccess: onClose })
  return (
    <Modal open title={tr('Сменить тариф')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="platform" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <Select value={plan} onChange={(e) => setPlan(e.target.value)}>{['starter', 'basic', 'business', 'pro', 'enterprise'].map((p) => <option key={p} value={p}>{p[0].toUpperCase() + p.slice(1)}</option>)}</Select>
    </Modal>
  )
}

function ExtendModal({ org, onClose }: { org: Org; onClose: () => void }) {
  const [months, setMonths] = useState('1')
  const [amount, setAmount] = useState(String(org.price_tiyn / 100))
  const [comment, setComment] = useState(tr('Оплата по счёту Kaspi'))
  const m = useMutation({ mutationFn: () => api.post(`/platform/organizations/${org.id}/extend`, { months: Number(months), amount_tiyn: Math.round(Number(amount) * 100) * Number(months), method: 'kaspi_invoice', comment }), onSuccess: onClose })
  return (
    <Modal open title={tr('Продлить подписку вручную')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="platform" loading={m.isPending} onClick={() => m.mutate()}>{tr('Продлить')}</Button></>}>
      <div className="grid grid-cols-2 gap-4">
        <Field label={tr('Месяцев')}><Input type="number" value={months} onChange={(e) => setMonths(e.target.value)} /></Field>
        <Field label={tr('Сумма за месяц, ₸')}><Input value={amount} onChange={(e) => setAmount(e.target.value)} /></Field>
        <Field label={tr('Комментарий')} className="col-span-2"><Input value={comment} onChange={(e) => setComment(e.target.value)} /></Field>
      </div>
    </Modal>
  )
}
