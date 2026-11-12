import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Download } from 'lucide-react'
import { api, download } from '../lib/api'
import { addDays, date, isoDay } from '../lib/format'
import { num, type TestConfig, type TestResult } from '../lib/tests'
import type { Employee, Group } from '../lib/types'
import { GradeChip } from '../components/Tests'
import { Button, Card, Empty, Field, Input, Select, Spinner, Stat } from '../components/ui'
import { tr } from '../lib/i18n'

// Результаты всех тестов учеников: за уроки и внесённые вручную, с фильтрами и выгрузкой в Excel.
export default function TestResults() {
  const [f, setF] = useState({ from: addDays(isoDay(), -30), to: isoDay(), test_type_id: '', group_id: '', teacher_id: '', source: '', q: '' })
  const [busy, setBusy] = useState(false)
  const qs = new URLSearchParams(Object.entries(f).filter(([, v]) => v) as [string, string][]).toString()
  const list = useQuery({ queryKey: ['tests', 'all', qs], queryFn: () => api.get<TestResult[]>(`/tests?limit=1000&${qs}`) })
  const cfg = useQuery({ queryKey: ['tests-config', 'archived'], queryFn: () => api.get<TestConfig>('/tests/config?archived=1') })
  const groups = useQuery({ queryKey: ['groups', 'all'], queryFn: () => api.get<Group[]>('/groups') })
  const teachers = useQuery({ queryKey: ['employees', 'teachers'], queryFn: () => api.get<Employee[]>('/employees?teachers=1') })
  const rows = list.data ?? []
  const withPercent = rows.filter((r) => r.percent != null)
  const avg = withPercent.length ? withPercent.reduce((a, r) => a + r.percent!, 0) / withPercent.length : null
  const set = (k: keyof typeof f, v: string) => setF({ ...f, [k]: v })
  const exportXlsx = async () => { setBusy(true); try { await download(`/tests?format=xlsx&${qs}`) } finally { setBusy(false) } }

  return (
    <div className="space-y-5 p-4 lg:p-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-bold">{tr('Тесты учеников')}</h1>
          <p className="mt-1 text-sm text-muted">{tr('Все результаты: баллы за уроки из отметки посещаемости и тесты, внесённые в карточке ученика.')}</p>
        </div>
        <Button onClick={exportXlsx} loading={busy} disabled={!rows.length}><Download className="size-4" />{tr('Выгрузить в Excel')}</Button>
      </div>

      <Card>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7">
          <Field label={tr('С')}><Input type="date" value={f.from} onChange={(e) => set('from', e.target.value)} /></Field>
          <Field label={tr('По')}><Input type="date" value={f.to} onChange={(e) => set('to', e.target.value)} /></Field>
          <Field label={tr('Тест')}>
            <Select value={f.test_type_id} onChange={(e) => set('test_type_id', e.target.value)}>
              <option value="">{tr('Все тесты')}</option>
              {cfg.data?.types.map((t) => <option key={t.id} value={t.id}>{t.name}{t.archived ? tr(' (архив)') : ''}</option>)}
            </Select>
          </Field>
          <Field label={tr('Группа')}>
            <Select value={f.group_id} onChange={(e) => set('group_id', e.target.value)}>
              <option value="">{tr('Все группы')}</option>
              {groups.data?.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </Select>
          </Field>
          <Field label={tr('Преподаватель')}>
            <Select value={f.teacher_id} onChange={(e) => set('teacher_id', e.target.value)}>
              <option value="">{tr('Все')}</option>
              {teachers.data?.map((t) => <option key={t.id} value={t.id}>{t.full_name}</option>)}
            </Select>
          </Field>
          <Field label={tr('Где внесён')}>
            <Select value={f.source} onChange={(e) => set('source', e.target.value)}>
              <option value="">{tr('Везде')}</option><option value="lesson">{tr('На уроке')}</option><option value="manual">{tr('В карточке')}</option>
            </Select>
          </Field>
          <Field label={tr('Ученик')}><Input value={f.q} onChange={(e) => set('q', e.target.value)} placeholder={tr('Поиск по ФИО')} /></Field>
        </div>
      </Card>

      <div className="grid gap-3 sm:grid-cols-3">
        <Stat label={tr('Результатов')} value={rows.length} />
        <Stat label={tr('Учеников')} value={new Set(rows.map((r) => r.student_id)).size} />
        <Stat label={tr('Средний процент')} value={avg != null ? `${num(Math.round(avg * 10) / 10)}%` : '—'} />
      </div>

      <Card bodyClass="!p-0">
        {!list.data ? <Spinner /> : rows.length === 0 ? <Empty title={tr('Результатов нет')} text={tr('Измените период или фильтры.')} /> : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="border-b border-line text-left text-[11px] font-semibold uppercase tracking-wide text-muted">
                {[tr('Дата'), tr('Ученик'), tr('Тест'), tr('Группа · преподаватель'), tr('По навыкам'), tr('Итог')].map((h) => <th key={h} className="whitespace-nowrap px-4 py-3">{h}</th>)}
              </tr></thead>
              <tbody className="divide-y divide-line">
                {rows.map((r) => (
                  <tr key={r.id} className="align-top">
                    <td className="whitespace-nowrap px-4 py-3 tabular">{date(r.taken_on)}{r.lesson_id && <div className="text-[11px] text-muted">{tr('на уроке')}</div>}</td>
                    <td className="px-4 py-3"><Link to={`/students/${r.student_id}`} className="font-medium text-brand-700 hover:underline">{r.student}</Link></td>
                    <td className="px-4 py-3">{r.test_type}</td>
                    <td className="px-4 py-3 text-[13px] text-muted">{[r.group, r.teacher].filter(Boolean).join(' · ') || '—'}</td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1">
                        {r.scores.map((s) => (
                          <span key={s.skill_id} className="inline-flex items-center gap-1 rounded-md border border-line px-1.5 py-0.5 text-xs">
                            <span className="text-muted">{s.skill}</span>{s.grade ? <b>{s.grade.name}</b> : <b className="tabular">{num(s.score)}/{num(s.max_score)}</b>}
                          </span>
                        ))}
                      </div>
                      {r.comment && <div className="mt-1 text-xs text-gray-600">{r.comment}</div>}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      {r.percent != null && <div className="font-bold tabular">{num(r.percent)}%<span className="ml-1 text-xs font-normal text-muted">{num(r.total_score)}/{num(r.max_score)}</span></div>}
                      <GradeChip g={r.grade} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {rows.length >= 1000 && <div className="border-t border-line px-4 py-2 text-xs text-muted">{tr('Показаны первые 1000 результатов — сузьте период или выгрузите в Excel.')}</div>}
          </div>
        )}
      </Card>
    </div>
  )
}
