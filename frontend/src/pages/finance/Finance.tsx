import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { addDays, date, dateTime, isoDay, money, phone } from '../../lib/format'
import { Badge, Button, Card, Input, Select, Spinner, Stat, Table, Tabs, paymentMethods } from '../../components/ui'
import { PaymentModal } from './PaymentModal'
import { tr } from '../../lib/i18n'

export default function Finance() {
  const { can, branchId } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as 'payments' | 'debtors' | 'summary') ?? 'payments'
  const [from, setFrom] = useState(addDays(isoDay(), -30))
  const [to, setTo] = useState(isoDay())
  const [method, setMethod] = useState('')
  const [pay, setPay] = useState<string | null>(null)
  const payments = useQuery({ queryKey: ['payments', from, to, method, branchId], queryFn: () => api.getMeta(`/payments?from=${from}&to=${addDays(to, 1)}&method=${method}&branch_id=${branchId}`), enabled: tab === 'payments' })
  const debtors = useQuery({ queryKey: ['debtors', branchId], queryFn: () => api.getMeta(`/debtors?branch_id=${branchId}`), enabled: tab === 'debtors' })
  const summary = useQuery({ queryKey: ['fin-summary', from, to, branchId], queryFn: () => api.get(`/finance/summary?from=${from}&to=${addDays(to, 1)}`), enabled: tab === 'summary' })

  return (
    <div className="mx-auto max-w-[1400px] p-4 lg:p-8">
      <div className="flex items-center justify-between">
        <h1 className="text-[26px] font-bold">{tr('Финансы')}</h1>
        {can('payments.create') && <Button variant="primary" onClick={() => setPay('')}><Plus className="size-4" />{tr('Платёж')}</Button>}
      </div>
      <Tabs className="mt-5" value={tab} onChange={(v) => setParams({ tab: v })} items={[{ value: 'payments', label: tr('Поступления') }, { value: 'debtors', label: tr('Должники') }, { value: 'summary', label: tr('Сводка') }]} />
      {tab !== 'debtors' && (
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Input type="date" className="!h-9 w-auto" value={from} onChange={(e) => setFrom(e.target.value)} /> —
          <Input type="date" className="!h-9 w-auto" value={to} onChange={(e) => setTo(e.target.value)} />
          {tab === 'payments' && <Select className="!h-9 w-auto" value={method} onChange={(e) => setMethod(e.target.value)}><option value="">{tr('Все способы')}</option>{Object.entries(paymentMethods).map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select>}
        </div>
      )}

      {tab === 'payments' && (payments.isLoading ? <Spinner /> : (
        <>
          <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Stat label={tr('Поступило за период')} value={money(payments.data?.meta.total_tiyn as number)} />
            <Stat label={tr('Платежей')} value={String(payments.data?.meta.count ?? 0)} />
          </div>
          <Card className="mt-4" bodyClass="!p-0">
            <Table head={[tr('Дата'), tr('Ученик'), tr('Сумма'), tr('Способ'), tr('Плательщик'), tr('Принял'), tr('Чек')]}>
              {(payments.data?.data as any[])?.map((p) => (
                <tr key={p.id} className="hover:bg-gray-50">
                  <td className="whitespace-nowrap px-4 py-3 text-[13px] text-muted">{dateTime(p.paid_at)}</td>
                  <td className="px-4 py-3"><Link to={`/students/${p.student_id}`} className="font-medium hover:text-brand-600">{p.student_name}</Link></td>
                  <td className="whitespace-nowrap px-4 py-3 font-semibold tabular">{money(p.amount_tiyn)}{p.refunded_tiyn > 0 && <Badge tone="red" className="ml-2">{tr('возврат')} {money(p.refunded_tiyn)}</Badge>}</td>
                  <td className="px-4 py-3 text-[13px]">{paymentMethods[p.method]}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{p.payer || '—'}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{p.received_by}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{p.receipt_number ?? '—'}</td>
                </tr>
              ))}
            </Table>
          </Card>
        </>
      ))}

      {tab === 'debtors' && (debtors.isLoading ? <Spinner /> : (
        <>
          <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Stat tone="warn" label={tr('Общая задолженность')} value={money(-(debtors.data?.meta.total_tiyn as number))} />
            <Stat label={tr('Должников')} value={String(debtors.data?.meta.count ?? 0)} />
          </div>
          <Card className="mt-4" bodyClass="!p-0">
            <Table head={[tr('Ученик'), tr('Группы'), tr('Долг'), tr('Долг с'), tr('Последняя оплата'), tr('Контакт плательщика'), '']}>
              {(debtors.data?.data as any[])?.map((d) => (
                <tr key={d.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3"><Link to={`/students/${d.id}`} className="font-medium hover:text-brand-600">{d.full_name}</Link><div className="text-xs text-muted">{phone(d.phone)}</div></td>
                  <td className="px-4 py-3 text-[13px]">{d.groups ?? '—'}</td>
                  <td className="whitespace-nowrap px-4 py-3 font-semibold text-red-700 tabular">{money(-d.balance_tiyn)}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{date(d.debt_since)}</td>
                  <td className="px-4 py-3 text-[13px] text-muted">{date(d.last_payment_at)}</td>
                  <td className="px-4 py-3 text-[13px]">{d.payer?.replace(/\+7(\d{3})(\d{3})(\d{2})(\d{2})/, '+7 $1 $2-$3-$4') ?? '—'}</td>
                  <td className="px-4 py-3 text-right">{can('payments.create') && <Button size="sm" onClick={() => setPay(d.id)}>{tr('Принять оплату')}</Button>}</td>
                </tr>
              ))}
            </Table>
          </Card>
        </>
      ))}

      {tab === 'summary' && summary.data && (
        <div className="mt-4 grid gap-6 lg:grid-cols-2">
          <div className="grid grid-cols-2 gap-4">
            <Stat label={tr('Поступления')} value={money(summary.data.income_tiyn)} />
            <Stat label={tr('Возвраты')} value={money(summary.data.refunds_tiyn)} />
            <Stat label={tr('Чистые поступления')} value={money(summary.data.income_tiyn - summary.data.refunds_tiyn)} />
          </div>
          <Card title={tr('По способам оплаты')}>
            {Object.entries(summary.data.by_method as Record<string, number>).sort((a, b) => b[1] - a[1]).map(([k, v]) => (
              <Bar key={k} label={paymentMethods[k] ?? k} value={v} max={Math.max(...Object.values(summary.data.by_method as Record<string, number>))} />
            ))}
          </Card>
          <Card title={tr('Начисления по курсам')} className="lg:col-span-2">
            {(summary.data.by_course as { course: string; charged_tiyn: number }[]).map((c) => <Bar key={c.course} label={c.course} value={c.charged_tiyn} max={summary.data.by_course[0]?.charged_tiyn ?? 1} />)}
          </Card>
        </div>
      )}
      {pay !== null && <PaymentModal studentId={pay || undefined} onClose={() => setPay(null)} />}
    </div>
  )
}

export const Bar = ({ label, value, max, fmt = money }: { label: string; value: number; max: number; fmt?: (v: number) => string }) => (
  <div className="py-2">
    <div className="flex justify-between text-sm"><span>{label}</span><span className="font-semibold tabular">{fmt(value)}</span></div>
    <div className="mt-1.5 h-2 overflow-hidden rounded-full bg-gray-100"><div className="h-full rounded-full bg-brand-600" style={{ width: `${(value / (max || 1)) * 100}%` }} /></div>
  </div>
)
