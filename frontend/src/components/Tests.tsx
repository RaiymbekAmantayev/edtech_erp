import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { date, isoDay } from '../lib/format'
import { autoGrade, gradeStyle, num, summarize, type GradeRef, type TestConfig, type TestResult } from '../lib/tests'
import { Button, Card, Empty, Field, Input, Modal, Select, Spinner, Textarea, cx } from './ui'

export const GradeChip = ({ g, className }: { g: GradeRef | null | undefined; className?: string }) =>
  g ? <span className={cx('inline-flex items-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-semibold whitespace-nowrap', className)} style={gradeStyle(g.color)}>{g.name}</span> : null

export function ResultRow({ r, onDelete, showStudent }: { r: TestResult; onDelete?: () => void; showStudent?: boolean }) {
  return (
    <div className="px-5 py-3.5">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2 font-medium">{showStudent ? r.student : r.test_type}<GradeChip g={r.grade} /></div>
          <div className="mt-0.5 text-[13px] text-muted">{[date(r.taken_on), showStudent && r.test_type, r.group, r.teacher].filter(Boolean).join(' · ')}</div>
        </div>
        {r.percent != null && <div className="text-right tabular"><div className="text-[17px] font-bold">{num(r.percent)}%</div><div className="text-xs text-muted">{num(r.total_score)} из {num(r.max_score)}</div></div>}
        {onDelete && <Button size="sm" variant="ghost" className="!text-red-600" onClick={onDelete}>Удалить</Button>}
      </div>
      {r.scores.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {r.scores.map((s) => (
            <span key={s.skill_id} className="inline-flex items-center gap-1.5 rounded-md border border-line px-2 py-1 text-xs">
              <span className="text-muted">{s.skill}</span>
              {s.grade ? <GradeChip g={s.grade} className="!px-1.5 !py-0" /> : <b className="tabular">{num(s.score)}<span className="font-normal text-muted">/{num(s.max_score)}</span></b>}
            </span>
          ))}
        </div>
      )}
      {r.comment && <div className="mt-2 text-sm text-gray-700">{r.comment}</div>}
    </div>
  )
}

// Вкладка «Тесты» карточки ученика: история результатов и ввод нового.
export function StudentTests({ studentId, groups }: { studentId: string; groups: { id: string; name: string }[] }) {
  const { can, me } = useAuth()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const q = useQuery({ queryKey: ['tests', 'student', studentId], queryFn: () => api.get<TestResult[]>(`/tests?student_id=${studentId}&limit=100`) })
  const del = useMutation({ mutationFn: (id: string) => api.del(`/tests/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['tests'] }) })
  const canDelete = (r: TestResult) => can('students.edit') || r.created_by === me?.user.id
  return (
    <>
      <Card className="mt-6" title="Результаты тестов" bodyClass="!px-0 !pb-0" action={can('tests.conduct') && <Button variant="primary" size="sm" onClick={() => setOpen(true)}>+ Внести результат</Button>}>
        {!q.data ? <Spinner /> : q.data.length === 0 ? <Empty title="Тестов пока нет" text="Результаты, внесённые здесь или преподавателем в Telegram, появятся в этом списке." /> : (
          <div className="divide-y divide-line border-t border-line">
            {q.data.map((r) => <ResultRow key={r.id} r={r} onDelete={canDelete(r) ? () => { if (confirm('Удалить результат теста?')) del.mutate(r.id) } : undefined} />)}
          </div>
        )}
      </Card>
      {open && <ResultModal studentId={studentId} groups={groups} onClose={() => setOpen(false)} onSaved={() => { setOpen(false); qc.invalidateQueries({ queryKey: ['tests'] }) }} />}
    </>
  )
}

function ResultModal({ studentId, groups, onClose, onSaved }: { studentId: string; groups: { id: string; name: string }[]; onClose: () => void; onSaved: () => void }) {
  const cfg = useQuery({ queryKey: ['tests-config', 'use'], queryFn: () => api.get<TestConfig>('/tests/config') })
  const [typeId, setTypeId] = useState('')
  const [groupId, setGroupId] = useState(groups.length === 1 ? groups[0].id : '')
  const [day, setDay] = useState(isoDay())
  const [scores, setScores] = useState<Record<string, string>>({})
  const [grade, setGrade] = useState('')
  const [comment, setComment] = useState('')
  const [error, setError] = useState('')
  const types = cfg.data?.types ?? []
  const grades = cfg.data?.grades ?? []
  const type = types.find((t) => t.id === typeId)
  const sum = type && type.scoring === 'score' ? summarize(type, scores) : null
  const auto = sum ? autoGrade(grades, sum.percent) : null
  const save = useMutation({
    mutationFn: () => api.post('/tests', {
      test_type_id: typeId, group_id: groupId || null, taken_on: day,
      items: [{
        student_id: studentId, grade_id: grade || null, comment,
        scores: type!.skills.map((s) => type!.scoring === 'score'
          ? { skill_id: s.skill_id, score: scores[s.skill_id]?.trim() ? Number(scores[s.skill_id].replace(',', '.')) : null }
          : { skill_id: s.skill_id, grade_id: scores[s.skill_id] || null }),
      }],
    }),
    onSuccess: onSaved,
    onError: (e) => setError(e instanceof ApiError && e.fields.some((f) => f.code === 'TOO_LARGE') ? 'Балл больше максимального для навыка' : e instanceof ApiError && e.fields.some((f) => f.field.endsWith('.scores')) ? 'Заполните хотя бы один навык' : (e as Error).message),
  })
  const filled = type ? (type.scoring === 'score' ? (sum?.filled ?? 0) > 0 : Object.values(scores).some(Boolean)) || !!grade : false
  return (
    <Modal open onClose={onClose} title="Результат теста" wide
      footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={save.isPending} disabled={!type || !filled} onClick={() => { setError(''); save.mutate() }}>Сохранить</Button></>}>
      {!cfg.data ? <Spinner /> : types.length === 0 ? <div className="py-6 text-center text-sm text-muted">Типы тестов не настроены. Администратор добавляет их в «Настройки → Тестирование».</div> : (
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Тип теста" required className="sm:col-span-3">
              <Select value={typeId} onChange={(e) => { setTypeId(e.target.value); setScores({}) }}>
                <option value="">Выберите тест</option>
                {types.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
              </Select>
            </Field>
            <Field label="Дата" required><Input type="date" value={day} max={isoDay()} onChange={(e) => setDay(e.target.value)} /></Field>
            <Field label="Группа" className="sm:col-span-2">
              <Select value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                <option value="">Без группы</option>
                {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </Select>
            </Field>
          </div>
          {type && (
            <>
              {type.description && <div className="rounded-lg bg-gray-50 p-3 text-sm text-gray-700">{type.description}</div>}
              <div className="divide-y divide-line rounded-lg border border-line">
                {type.skills.map((s) => (
                  <div key={s.skill_id} className="flex items-center gap-3 px-3 py-2">
                    <div className="flex-1 text-sm">{s.name}</div>
                    {type.scoring === 'score' ? (
                      <label className="flex items-center gap-2 text-sm text-muted">
                        <Input className="!w-20 text-right" inputMode="decimal" value={scores[s.skill_id] ?? ''} invalid={Number((scores[s.skill_id] ?? '').replace(',', '.')) > s.max_score}
                          onChange={(e) => setScores({ ...scores, [s.skill_id]: e.target.value })} />из {num(s.max_score)}
                      </label>
                    ) : (
                      <Select className="!w-44" value={scores[s.skill_id] ?? ''} onChange={(e) => setScores({ ...scores, [s.skill_id]: e.target.value })}>
                        <option value="">—</option>
                        {grades.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                      </Select>
                    )}
                  </div>
                ))}
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Итоговая оценка" hint={type.scoring === 'score' ? (sum?.percent != null ? `${num(sum.total)} из ${num(sum.max)} — ${num(sum.percent)}%${auto ? `, по порогам: ${auto.name}` : ''}` : 'Рассчитается по баллам') : undefined}>
                  <Select value={grade} onChange={(e) => setGrade(e.target.value)}>
                    <option value="">{type.scoring === 'score' ? 'Автоматически' : 'Без итоговой оценки'}</option>
                    {grades.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
                  </Select>
                </Field>
                <Field label="Комментарий"><Textarea rows={2} className="!min-h-10" value={comment} onChange={(e) => setComment(e.target.value)} /></Field>
              </div>
            </>
          )}
          {error && <div className="text-sm text-red-700">{error}</div>}
        </div>
      )}
    </Modal>
  )
}
