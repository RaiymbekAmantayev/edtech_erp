import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Card, Spinner, Stat, studentStatus } from '../components/ui'
import { Bar } from './finance/Finance'

export default function Reports() {
  const { can } = useAuth()
  const q = useQuery({ queryKey: ['report-students'], queryFn: () => api.get<any>('/reports/students') })
  const att = useQuery({ queryKey: ['att-report-30'], queryFn: () => api.get<any[]>('/attendance/report'), enabled: can('attendance.view') })
  if (!q.data) return <Spinner />
  const d = q.data
  const maxMonth = Math.max(...d.months.map((m: any) => Math.max(m.joined, m.left, m.leads)), 1)
  const rateTotal = att.data?.reduce((a, r) => a + r.present, 0) ?? 0
  const allTotal = att.data?.reduce((a, r) => a + r.present + r.absent + r.excused, 0) ?? 0
  return (
    <div className="mx-auto max-w-[1300px] space-y-6 p-4 lg:p-8">
      <h1 className="text-[26px] font-bold">Отчёты</h1>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Object.entries(d.by_status as Record<string, number>).map(([k, v]) => <Stat key={k} label={studentStatus[k]?.[0] ?? k} value={v} />)}
        {allTotal > 0 && <Stat label="Посещаемость за 30 дней" value={`${Math.round((rateTotal / allTotal) * 100)}%`} />}
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <Card title="Движение учеников по месяцам">
          <div className="flex h-48 items-end gap-4">
            {d.months.map((m: any) => (
              <div key={m.month} className="flex flex-1 flex-col items-center gap-1">
                <div className="flex h-40 w-full items-end justify-center gap-1">
                  {[['joined', 'bg-brand-600'], ['left', 'bg-orange-400'], ['leads', 'bg-gray-300']].map(([k, c]) => (
                    <div key={k} title={`${k}: ${m[k]}`} className={`w-3 rounded-t ${c}`} style={{ height: `${(m[k] / maxMonth) * 150 + 2}px` }} />
                  ))}
                </div>
                <span className="text-xs text-muted">{m.month.slice(5)}.{m.month.slice(2, 4)}</span>
              </div>
            ))}
          </div>
          <div className="mt-3 flex gap-4 text-xs text-muted"><span>■ <span className="text-brand-600">пришли</span></span><span>■ <span className="text-orange-500">выбыли</span></span><span>■ лиды</span></div>
        </Card>
        <Card title="Лиды по источникам · конверсия">
          {d.sources.map((s: any) => <Bar key={s.source} label={`${s.source} · ${s.won} из ${s.leads} (${Math.round((s.won / s.leads) * 100)}%)`} value={s.leads} max={d.sources[0]?.leads ?? 1} fmt={(v) => String(v)} />)}
        </Card>
        <Card title="Причины выбытия (отток)">
          {d.leave_reasons.length === 0 ? <div className="text-sm text-muted">Выбывших пока нет</div> : d.leave_reasons.map((r: any) => <Bar key={r.reason} label={r.reason} value={r.count} max={d.leave_reasons[0].count} fmt={(v) => String(v)} />)}
        </Card>
        {can('attendance.view') && (
          <Card title="Посещаемость по группам · 30 дней">
            {att.data?.filter((r) => r.rate != null).sort((a, b) => a.rate - b.rate).map((r) => <Bar key={r.group_id} label={r.group} value={Math.round(r.rate)} max={100} fmt={(v) => v + '%'} />)}
          </Card>
        )}
      </div>
      <div className="text-sm text-muted">Должники и поступления — в разделе <Link to="/finance" className="text-brand-600">Финансы</Link>. Экспорт учеников в Excel — в разделе <Link to="/students" className="text-brand-600">Ученики</Link>.</div>
    </div>
  )
}
