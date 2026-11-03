import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { date, money } from '../../lib/format'
import { Button, Card, Field, Input, Spinner, Stat, Table } from '../../components/ui'

export function PlatformPlans() {
  const q = useQuery({ queryKey: ['platform-plans'], queryFn: () => api.get<any[]>('/platform/plans') })
  return (
    <div className="mx-auto max-w-5xl p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">Тарифы и лимиты</h1>
      <Card className="mt-5" bodyClass="!p-0">
        {q.isLoading ? <Spinner /> : (
          <Table head={['Тариф', 'Цена/мес', 'Учеников', 'Филиалов', 'Сотрудников', 'Полей на сущность', 'Хранилище', 'SMS', 'Организаций']}>
            {q.data?.map((p) => (
              <tr key={p.code}>
                <td className="px-4 py-3 font-semibold">{p.name}</td><td className="px-4 py-3 tabular">{p.price_tiyn ? money(p.price_tiyn) : 'договор'}</td>
                <td className="px-4 py-3 tabular">{p.max_students ?? '∞'}</td><td className="px-4 py-3 tabular">{p.max_branches ?? '∞'}</td><td className="px-4 py-3 tabular">{p.max_staff ?? '∞'}</td>
                <td className="px-4 py-3 tabular">{p.max_custom_fields}</td><td className="px-4 py-3 tabular">{p.storage_gb} ГБ</td><td className="px-4 py-3 tabular">{p.sms_per_month}</td><td className="px-4 py-3 tabular">{p.organizations}</td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      <p className="mt-3 text-xs text-muted">Цены — предварительная гипотеза, проверяется на пилотных клиентах (ТЗ 23.2).</p>
    </div>
  )
}

export function PlatformSystem() {
  const q = useQuery({ queryKey: ['platform-system'], queryFn: () => api.get<any>('/platform/system'), refetchInterval: 10_000 })
  if (!q.data) return <Spinner />
  const s = q.data
  return (
    <div className="mx-auto max-w-5xl p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">Мониторинг системы</h1>
      <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Stat label="Очередь outbox" value={s.outbox_backlog} sub={<span className="text-muted">событий ждут обработки</span>} />
        <Stat label="Событий с ошибкой" value={s.outbox_failed} sub={<span className="text-muted">10+ неудачных попыток</span>} />
        <Stat label="Ошибки уведомлений за 24 ч" value={s.notifications_failed_24h} />
        <Stat label="Размер БД" value={s.db_size} />
        <Stat label="Соединения пула" value={`${s.db_pool.acquired} / ${s.db_pool.total}`} sub={<span className="text-muted">занято / всего, свободно {s.db_pool.idle}</span>} />
      </div>
      <p className="mt-4 text-xs text-muted">Prometheus/Grafana и Loki подключаются на этапе Production Hardening; здесь — быстрые показатели из самой системы.</p>
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
      <h1 className="text-[26px] font-bold">Производственный календарь РК</h1>
      <Card className="mt-5" title="Добавить день (включая переносы по постановлениям Правительства)">
        <div className="grid gap-3 sm:grid-cols-[1fr_2fr_auto_auto] sm:items-end">
          <Field label="Дата"><Input type="date" value={f.day} onChange={(e) => setF({ ...f, day: e.target.value })} /></Field>
          <Field label="Название"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
          <label className="flex h-10 items-center gap-2 text-sm"><input type="checkbox" checked={f.is_workday} onChange={(e) => setF({ ...f, is_workday: e.target.checked })} />рабочий (перенос)</label>
          <Button variant="platform" loading={m.isPending} onClick={() => m.mutate()}>Добавить</Button>
        </div>
      </Card>
      <Card className="mt-4" bodyClass="!p-0">
        <Table head={['Дата', 'Название', 'Тип']}>
          {q.data?.map((h) => <tr key={h.id}><td className="px-4 py-2.5 tabular">{date(h.day + 'T12:00:00+05:00')}</td><td className="px-4 py-2.5">{h.name}</td><td className="px-4 py-2.5 text-[13px] text-muted">{h.is_workday ? 'рабочий день' : 'выходной'}</td></tr>)}
        </Table>
      </Card>
    </div>
  )
}
