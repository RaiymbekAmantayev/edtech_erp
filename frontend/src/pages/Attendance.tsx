import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { addDays, dateTime, dayStart, isoDay, plural, time } from '../lib/format'
import type { Lesson } from '../lib/types'
import { Badge, Card, Input, Segmented, Spinner, StatusBadge, Table, attendanceStatus, lessonState } from '../components/ui'
import { tr } from '../lib/i18n'

export default function Attendance() {
  const { can, branchId, isTeacherOnly } = useAuth()
  const [tab, setTab] = useState<'today' | 'unmarked' | 'report' | 'changes'>('today')
  const [from, setFrom] = useState(addDays(isoDay(), -30))
  const [to, setTo] = useState(isoDay())
  const today = useQuery({ queryKey: ['lessons', 'today-att', branchId], queryFn: () => api.get<Lesson[]>(`/lessons?from=${dayStart(isoDay()).toISOString()}&to=${dayStart(addDays(isoDay(), 1)).toISOString()}&branch_id=${branchId}&hide_cancelled=1`), enabled: tab === 'today' })
  const unmarked = useQuery({ queryKey: ['lessons', 'unmarked', branchId], queryFn: () => api.get<Lesson[]>(`/lessons?unmarked=1&branch_id=${branchId}`), enabled: tab === 'unmarked' })
  const report = useQuery({ queryKey: ['att-report', from, to], queryFn: () => api.get<any[]>(`/attendance/report?from=${from}&to=${addDays(to, 1)}`), enabled: tab === 'report' })
  const changes = useQuery({ queryKey: ['att-changes'], queryFn: () => api.get<any[]>('/attendance/history'), enabled: tab === 'changes' })
  const lessons = tab === 'today' ? today : unmarked

  return (
    <div className="mx-auto max-w-[1300px] p-4 lg:p-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-[26px] font-bold">{tr('Посещаемость')}</h1>
        <Segmented value={tab} onChange={setTab} items={[
          { value: 'today', label: tr('Сегодня') }, { value: 'unmarked', label: tr('Требует отметки') },
          ...(can('attendance.view') && !isTeacherOnly ? [{ value: 'report' as const, label: tr('Отчёт по группам') }, { value: 'changes' as const, label: tr('Изменения') }] : []),
        ]} />
      </div>
      {(tab === 'today' || tab === 'unmarked') && (lessons.isLoading ? <Spinner /> : (
        <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {(lessons.data ?? []).length === 0 && <div className="col-span-full py-12 text-center text-sm text-muted">{tab === 'unmarked' ? tr('Все прошедшие занятия отмечены 👍') : tr('Сегодня занятий нет')}</div>}
          {lessons.data?.map((l) => (
            <Link key={l.id} to={`/attendance/${l.id}`} className="rounded-xl border border-line bg-white p-4 hover:border-gray-300">
              <div className="flex items-start justify-between gap-2">
                <div><div className="font-semibold">{l.group_name}</div><div className="text-[13px] text-muted tabular">{tab === 'unmarked' ? dateTime(l.starts_at) : `${time(l.starts_at)}–${time(l.ends_at)}`} · {l.classroom_name}</div></div>
                <StatusBadge map={lessonState} value={l.state} />
              </div>
              <div className="mt-3 flex items-center justify-between text-[13px]">
                <span className="text-muted">{l.teacher_name} · {l.students} {plural(l.students, tr('ученик'), tr('ученика'), tr('учеников'))}{l.trials ? tr(' + {0} пробный', l.trials) : ''}</span>
                <span className="font-medium text-brand-600">{l.status === 'done' ? tr('Открыть') : tr('Отметить →')}</span>
              </div>
            </Link>
          ))}
        </div>
      ))}
      {tab === 'report' && (
        <>
          <div className="mt-5 flex items-center gap-2"><Input type="date" className="!h-9 w-auto" value={from} onChange={(e) => setFrom(e.target.value)} /> — <Input type="date" className="!h-9 w-auto" value={to} onChange={(e) => setTo(e.target.value)} /></div>
          <Card className="mt-4" bodyClass="!p-0">
            {report.isLoading ? <Spinner /> : (
              <Table head={[tr('Группа'), tr('Преподаватель'), tr('Проведено'), tr('Без отметки'), tr('Был/опоздал'), tr('Не был'), tr('Уваж.'), tr('Посещаемость')]}>
                {report.data?.map((r) => (
                  <tr key={r.group_id}>
                    <td className="px-4 py-3 font-medium"><Link to={`/groups/${r.group_id}`} className="hover:text-brand-600">{r.group}</Link></td>
                    <td className="px-4 py-3 text-[13px]">{r.teacher}</td>
                    <td className="px-4 py-3 tabular">{r.lessons_done}</td>
                    <td className="px-4 py-3 tabular">{r.lessons_unmarked > 0 ? <Badge tone="red">{r.lessons_unmarked}</Badge> : 0}</td>
                    <td className="px-4 py-3 tabular">{r.present}</td><td className="px-4 py-3 tabular">{r.absent}</td><td className="px-4 py-3 tabular">{r.excused}</td>
                    <td className="px-4 py-3 font-semibold tabular">{r.rate != null ? `${Math.round(r.rate)}%` : '—'}</td>
                  </tr>
                ))}
              </Table>
            )}
          </Card>
        </>
      )}
      {tab === 'changes' && (
        <Card className="mt-5" bodyClass="!p-0">
          <Table head={[tr('Когда'), tr('Ученик'), tr('Занятие'), tr('Изменение'), tr('Причина'), tr('Кто')]}>
            {changes.data?.map((c, i) => (
              <tr key={i}>
                <td className="px-4 py-3 text-[13px] text-muted">{dateTime(c.changed_at)}</td>
                <td className="px-4 py-3">{c.student}</td>
                <td className="px-4 py-3 text-[13px]">{c.group}, {dateTime(c.lesson_at)}</td>
                <td className="px-4 py-3 text-[13px]">{attendanceStatus[c.old]?.[0]} → {attendanceStatus[c.new]?.[0]}</td>
                <td className="px-4 py-3 text-[13px]">{c.reason ?? '—'}</td>
                <td className="px-4 py-3 text-[13px]">{c.user}</td>
              </tr>
            ))}
          </Table>
          {changes.data?.length === 0 && <div className="py-10 text-center text-sm text-muted">{tr('Отметки не исправлялись')}</div>}
        </Card>
      )}
    </div>
  )
}
