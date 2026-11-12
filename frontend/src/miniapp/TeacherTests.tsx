import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { ApiError, api } from '../lib/api'
import { dateShort, isoDay, plural } from '../lib/format'
import { autoGrade, gradeStyle, num, summarize, useTeacherTestConfig, type TestResult } from '../lib/tests'
import { Avatar, Spinner, cx } from '../components/ui'

type Group = { id: string; name: string; course: string; teacher: string | null; students: number }

// Вкладка «Тесты» кабинета преподавателя: выбор группы и последние внесённые результаты.
export function TeacherTestsTab() {
  const nav = useNavigate()
  const groups = useQuery({ queryKey: ['tests-groups'], queryFn: () => api.get<Group[]>('/tests/groups') })
  const recent = useQuery({ queryKey: ['tests', 'teacher'], queryFn: () => api.get<TestResult[]>('/tests?limit=30') })
  if (!groups.data) return <Spinner />
  return (
    <>
      <section className="mb-5">
        <h2 className="mb-2 px-1 text-[13px] font-semibold uppercase tracking-wide text-muted">Провести тест</h2>
        {groups.data.length === 0 ? <div className="rounded-2xl border border-dashed border-line p-6 text-center text-sm text-muted">У вас нет активных групп</div> : (
          <div className="divide-y divide-line rounded-2xl border border-line bg-white">
            {groups.data.map((g) => (
              <button key={g.id} onClick={() => nav(`test/${g.id}`)} className="flex w-full items-center gap-3 px-4 py-3.5 text-left active:bg-gray-50">
                <div className="min-w-0 flex-1"><div className="truncate text-[15px] font-semibold">{g.name}</div><div className="truncate text-[13px] text-muted">{g.course} · {g.students} {plural(g.students, 'ученик', 'ученика', 'учеников')}</div></div>
                <ChevronRight className="size-5 text-muted" />
              </button>
            ))}
          </div>
        )}
      </section>
      <section>
        <h2 className="mb-2 px-1 text-[13px] font-semibold uppercase tracking-wide text-muted">Последние результаты</h2>
        {!recent.data ? <Spinner /> : recent.data.length === 0 ? <div className="rounded-2xl border border-dashed border-line p-6 text-center text-sm text-muted">Результатов пока нет</div> : (
          <div className="divide-y divide-line rounded-2xl border border-line bg-white">
            {recent.data.map((r) => (
              <div key={r.id} className="flex items-center gap-3 px-4 py-3">
                <div className="w-12 shrink-0 text-[13px] text-muted tabular">{dateShort(r.taken_on)}</div>
                <div className="min-w-0 flex-1"><div className="truncate text-[15px]">{r.student}</div><div className="truncate text-[12px] text-muted">{r.test_type}{r.group && ` · ${r.group}`}</div></div>
                <div className="text-right">
                  {r.percent != null && <div className="text-[15px] font-bold tabular">{num(r.percent)}%</div>}
                  {r.grade && <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-semibold" style={gradeStyle(r.grade.color)}>{r.grade.name}</span>}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </>
  )
}

// Ввод результатов теста для всей группы с телефона.
export function TestEntry() {
  const { groupId } = useParams()
  const nav = useNavigate()
  const qc = useQueryClient()
  const cfg = useTeacherTestConfig()
  const roster = useQuery({ queryKey: ['tests-roster', groupId], queryFn: () => api.get<{ group: { id: string; name: string }; students: { id: string; full_name: string }[] }>('/tests/roster?group_id=' + groupId) })
  const [typeId, setTypeId] = useState('')
  const [day, setDay] = useState(isoDay())
  // ученик → навык → значение (балл или id оценки)
  const [vals, setVals] = useState<Record<string, Record<string, string>>>({})
  const [comments, setComments] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const types = cfg.data?.types ?? []
  const grades = cfg.data?.grades ?? []
  const type = types.find((t) => t.id === typeId) ?? (types.length === 1 ? types[0] : undefined)
  const students = roster.data?.students ?? []
  const filledOf = (sid: string) => Object.values(vals[sid] ?? {}).filter((v) => v.trim() !== '').length
  const ready = students.filter((s) => filledOf(s.id) > 0)
  const over = type?.scoring === 'score' && students.some((s) => type.skills.some((k) => Number((vals[s.id]?.[k.skill_id] ?? '').replace(',', '.')) > k.max_score))
  const save = useMutation({
    mutationFn: () => api.post('/tests', {
      test_type_id: type!.id, group_id: groupId, taken_on: day,
      items: ready.map((s) => ({
        student_id: s.id, comment: comments[s.id] ?? '',
        scores: type!.skills.map((k) => {
          const v = (vals[s.id]?.[k.skill_id] ?? '').trim()
          return type!.scoring === 'score' ? { skill_id: k.skill_id, score: v ? Number(v.replace(',', '.')) : null } : { skill_id: k.skill_id, grade_id: v || null }
        }),
      })),
    }),
    onSuccess: () => { setSaved(true); qc.invalidateQueries({ queryKey: ['tests'] }); setTimeout(() => nav(-1), 900) },
    onError: (e) => setError(e instanceof ApiError && e.fields.some((f) => f.code === 'TOO_LARGE') ? 'Есть балл больше максимального' : (e as Error).message),
  })
  if (cfg.isLoading || roster.isLoading) return <Spinner className="h-full" />
  if (roster.isError) return <div className="p-10 text-center text-muted">{(roster.error as Error).message}</div>
  const set = (sid: string, kid: string, v: string) => setVals({ ...vals, [sid]: { ...vals[sid], [kid]: v } })
  const inputCls = 'h-12 w-full rounded-xl border border-line bg-white px-3 text-[16px] outline-none focus:border-brand-500'

  return (
    <div className="mx-auto flex h-full max-w-xl flex-col bg-page">
      <header className="border-b border-line bg-white px-4 pb-4 pt-5">
        <div className="flex items-start gap-3">
          <button onClick={() => nav(-1)} className="flex size-11 items-center justify-center rounded-full hover:bg-gray-100" aria-label="Назад"><ChevronLeft className="size-6" /></button>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-xl font-bold">{roster.data!.group.name}</h1>
            <div className="text-sm text-muted">Результаты теста</div>
          </div>
        </div>
        {types.length === 0 ? null : (
          <>
            <div className="-mx-1 mt-4 flex gap-2 overflow-x-auto px-1">
              {types.map((t) => (
                <button key={t.id} onClick={() => { setTypeId(t.id); setVals({}) }}
                  className={cx('h-10 shrink-0 rounded-full border px-4 text-sm font-medium', type?.id === t.id ? 'border-brand-600 bg-brand-600 text-white' : 'border-line bg-white')}>{t.name}</button>
              ))}
            </div>
            <label className="mt-3 flex items-center gap-3 text-sm text-muted">Дата теста
              <input type="date" value={day} max={isoDay()} onChange={(e) => setDay(e.target.value)} className="h-10 flex-1 rounded-xl border border-line px-3 text-[15px] text-ink" />
            </label>
          </>
        )}
      </header>

      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        {types.length === 0 && <div className="rounded-xl bg-gray-100 p-4 text-sm text-gray-700">Администратор ещё не открыл тесты для Telegram-кабинета.</div>}
        {types.length > 0 && !type && <div className="rounded-xl bg-amber-50 p-4 text-sm text-amber-900">Выберите тест вверху.</div>}
        {type?.description && <div className="rounded-xl bg-white p-4 text-sm text-gray-700">{type.description}</div>}
        {type && students.length === 0 && <div className="py-10 text-center text-sm text-muted">В группе нет учеников</div>}
        {type && students.map((s) => {
          const sum = type.scoring === 'score' ? summarize(type, vals[s.id] ?? {}) : null
          const auto = sum ? autoGrade(grades, sum.percent) : null
          return (
            <div key={s.id} className="rounded-2xl border border-line bg-white p-4">
              <div className="flex items-center gap-3">
                <Avatar name={s.full_name} tone="gray" />
                <div className="min-w-0 flex-1 truncate text-[15px] font-semibold">{s.full_name}</div>
                {sum && sum.filled > 0 && (
                  <div className="text-right">
                    <div className="text-[15px] font-bold tabular">{num(sum.percent)}%</div>
                    {auto && <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-semibold" style={gradeStyle(auto.color)}>{auto.name}</span>}
                  </div>
                )}
              </div>
              <div className="mt-3 grid grid-cols-2 gap-2">
                {type.skills.map((k) => {
                  const v = vals[s.id]?.[k.skill_id] ?? ''
                  return (
                    <label key={k.skill_id} className="block">
                      <span className="mb-1 block truncate text-[12px] text-muted">{k.name}{type.scoring === 'score' && ` · из ${num(k.max_score)}`}</span>
                      {type.scoring === 'score' ? (
                        <input inputMode="decimal" value={v} onChange={(e) => set(s.id, k.skill_id, e.target.value.replace(/[^\d.,]/g, ''))}
                          className={cx(inputCls, 'text-center tabular', Number(v.replace(',', '.')) > k.max_score && 'border-red-400 bg-red-50')} />
                      ) : (
                        <select value={v} onChange={(e) => set(s.id, k.skill_id, e.target.value)} className={inputCls}>
                          <option value="">—</option>
                          {grades.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                        </select>
                      )}
                    </label>
                  )
                })}
              </div>
              {filledOf(s.id) > 0 && (
                <input value={comments[s.id] ?? ''} onChange={(e) => setComments({ ...comments, [s.id]: e.target.value })} placeholder="Комментарий (необязательно)" className={cx(inputCls, 'mt-2 text-[15px]')} />
              )}
            </div>
          )
        })}
      </div>

      {type && students.length > 0 && (
        <footer className="space-y-2 border-t border-line bg-white p-4">
          {error && <div className="text-sm text-red-700">{error}</div>}
          <button disabled={!ready.length || over || save.isPending || saved} onClick={() => { setError(''); save.mutate() }}
            className={cx('h-14 w-full rounded-xl text-[17px] font-semibold text-white transition-colors disabled:opacity-60', saved ? 'bg-green-600' : 'bg-brand-600 active:bg-brand-700')}>
            {saved ? 'Сохранено ✓' : save.isPending ? 'Сохранение…' : ready.length ? `Сохранить · ${ready.length} из ${students.length}` : 'Введите результаты'}
          </button>
        </footer>
      )}
    </div>
  )
}
