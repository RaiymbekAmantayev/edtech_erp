import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { date, money } from '../../lib/format'
import { Button, Card, Field, Input, Spinner, Stat, Table } from '../../components/ui'
import { tr } from '../../lib/i18n'

export function PlatformPlans() {
  const q = useQuery({ queryKey: ['platform-plans'], queryFn: () => api.get<any[]>('/platform/plans') })
  return (
    <div className="mx-auto max-w-5xl p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">{tr('Тарифы и лимиты')}</h1>
      <Card className="mt-5" bodyClass="!p-0">
        {q.isLoading ? <Spinner /> : (
          <Table head={[tr('Тариф'), tr('Цена/мес'), tr('Учеников'), tr('Филиалов'), tr('Сотрудников'), tr('Полей на сущность'), tr('Хранилище'), 'SMS', tr('Организаций')]}>
            {q.data?.map((p) => (
              <tr key={p.code}>
                <td className="px-4 py-3 font-semibold">{p.name}</td><td className="px-4 py-3 tabular">{p.price_tiyn ? money(p.price_tiyn) : tr('договор')}</td>
                <td className="px-4 py-3 tabular">{p.max_students ?? '∞'}</td><td className="px-4 py-3 tabular">{p.max_branches ?? '∞'}</td><td className="px-4 py-3 tabular">{p.max_staff ?? '∞'}</td>
                <td className="px-4 py-3 tabular">{p.max_custom_fields}</td><td className="px-4 py-3 tabular">{p.storage_gb} {tr('ГБ')}</td><td className="px-4 py-3 tabular">{p.sms_per_month}</td><td className="px-4 py-3 tabular">{p.organizations}</td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      <p className="mt-3 text-xs text-muted">{tr('Цены — предварительная гипотеза, проверяется на пилотных клиентах (ТЗ 23.2).')}</p>
    </div>
  )
}

export function PlatformSystem() {
  const q = useQuery({ queryKey: ['platform-system'], queryFn: () => api.get<any>('/platform/system'), refetchInterval: 10_000 })
  if (!q.data) return <Spinner />
  const s = q.data
  return (
    <div className="mx-auto max-w-5xl p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">{tr('Мониторинг системы')}</h1>
      <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Stat label={tr('Очередь outbox')} value={s.outbox_backlog} sub={<span className="text-muted">{tr('событий ждут обработки')}</span>} />
        <Stat label={tr('Событий с ошибкой')} value={s.outbox_failed} sub={<span className="text-muted">{tr('10+ неудачных попыток')}</span>} />
        <Stat label={tr('Ошибки уведомлений за 24 ч')} value={s.notifications_failed_24h} />
        <Stat label={tr('Размер БД')} value={s.db_size} />
        <Stat label={tr('Соединения пула')} value={`${s.db_pool.acquired} / ${s.db_pool.total}`} sub={<span className="text-muted">{tr('занято / всего, свободно')} {s.db_pool.idle}</span>} />
      </div>
      <p className="mt-4 text-xs text-muted">{tr('Prometheus/Grafana и Loki подключаются на этапе Production Hardening; здесь — быстрые показатели из самой системы.')}</p>
    </div>
  )
}

export function PlatformHolidays() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['platform-holidays'], queryFn: () => api.get<any[]>('/platform/holidays') })
  const [f, setF] = useState({ day: '', name: '', is_workday: false })
  const m = useMutation({ mutationFn: () => api.post('/platform/holidays', f), onSuccess: () => { setF({ day: '', name: '', is_workday: false }); qc.invalidateQueries({ queryKey: ['platform-holidays'] }) } })
  return (
    <div className="mx-auto max-w-4xl p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">{tr('Производственный календарь РК')}</h1>
      <Card className="mt-5" title={tr('Добавить день (включая переносы по постановлениям Правительства)')}>
        <div className="grid gap-3 sm:grid-cols-[1fr_2fr_auto_auto] sm:items-end">
          <Field label={tr('Дата')}><Input type="date" value={f.day} onChange={(e) => setF({ ...f, day: e.target.value })} /></Field>
          <Field label={tr('Название')}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
          <label className="flex h-10 items-center gap-2 text-sm"><input type="checkbox" checked={f.is_workday} onChange={(e) => setF({ ...f, is_workday: e.target.checked })} />{tr('рабочий (перенос)')}</label>
          <Button variant="platform" loading={m.isPending} onClick={() => m.mutate()}>{tr('Добавить')}</Button>
        </div>
      </Card>
      <Card className="mt-4" bodyClass="!p-0">
        <Table head={[tr('Дата'), tr('Название'), tr('Тип')]}>
          {q.data?.map((h) => <tr key={h.id}><td className="px-4 py-2.5 tabular">{date(h.day + 'T12:00:00+05:00')}</td><td className="px-4 py-2.5">{h.name}</td><td className="px-4 py-2.5 text-[13px] text-muted">{h.is_workday ? tr('рабочий день') : tr('выходной')}</td></tr>)}
        </Table>
      </Card>
    </div>
  )
}
