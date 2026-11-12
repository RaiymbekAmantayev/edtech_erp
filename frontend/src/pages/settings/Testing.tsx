import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Send } from 'lucide-react'
import { ApiError, api, errorsByField } from '../../lib/api'
import { gradeStyle, num, scoringLabel, type TestConfig, type TestGrade, type TestSkill, type TestType } from '../../lib/tests'
import { Badge, Banner, Button, Card, Checkbox, Empty, Field, Input, Modal, Segmented, Spinner, Tabs, Textarea, Toast, cx } from '../../components/ui'

const palette = ['#16a34a', '#0d9488', '#2563eb', '#7c3aed', '#d97706', '#ea580c', '#dc2626', '#64748b']

// Настройки тестирования школы: типы тестов, навыки и шкала оценок (по образцу HolliHope).
export default function Testing() {
  const [tab, setTab] = useState<'types' | 'skills' | 'grades'>('types')
  const [archived, setArchived] = useState(false)
  const q = useQuery({ queryKey: ['tests-config', 'admin', archived], queryFn: () => api.get<TestConfig>('/tests/config' + (archived ? '?archived=1' : '')) })
  const [toast, setToast] = useState('')
  if (!q.data) return <Spinner />
  const c = q.data
  return (
    <div className="max-w-4xl space-y-5">
      <div>
        <h1 className="text-[22px] font-bold">Тестирование</h1>
        <p className="mt-1 text-sm text-muted">Настройте свои тесты: какие навыки проверяются, сколько баллов за каждый и какая оценка выставляется. Результаты вносят преподаватели — в карточке ученика или в Telegram-кабинете.</p>
      </div>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <Tabs value={tab} onChange={setTab} className="flex-1" items={[
          { value: 'types', label: 'Типы тестов', count: c.types.filter((t) => !t.archived).length },
          { value: 'skills', label: 'Навыки', count: c.skills.filter((s) => !s.archived).length },
          { value: 'grades', label: 'Оценки', count: c.grades.filter((g) => !g.archived).length },
        ]} />
        <Checkbox checked={archived} onChange={setArchived} label="Показать архив" />
      </div>
      {tab === 'types' && <Types c={c} onToast={setToast} onSkills={() => setTab('skills')} />}
      {tab === 'skills' && <Skills skills={c.skills} onToast={setToast} />}
      {tab === 'grades' && <Grades grades={c.grades} onToast={setToast} />}
      {toast && <Toast text={toast} onDone={() => setToast('')} />}
    </div>
  )
}

function useRefresh() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: ['tests-config'] })
}

// ---------------------------------------------------------------- типы тестов

function Types({ c, onToast, onSkills }: { c: TestConfig; onToast: (s: string) => void; onSkills: () => void }) {
  const refresh = useRefresh()
  const [edit, setEdit] = useState<TestType | 'new' | null>(null)
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) => api.patch(`/test-types/${id}`, body),
    onSuccess: () => { refresh(); onToast('Сохранено') },
  })
  const activeSkills = c.skills.filter((s) => !s.archived)
  return (
    <>
      <Card title="Типы тестов" bodyClass="!px-0 !pb-0" action={<Button variant="primary" size="sm" disabled={!activeSkills.length} onClick={() => setEdit('new')}>+ Тип теста</Button>}>
        {!activeSkills.length && <div className="px-5 pb-4"><Banner tone="blue" action={<Button size="sm" onClick={onSkills}>Добавить навыки</Button>}>Сначала добавьте навыки, которые проверяют тесты: например «Grammar», «Listening» или «Алгебра».</Banner></div>}
        {c.types.length === 0 ? <Empty title="Типов тестов пока нет" text="Например: «Входное тестирование», «Промежуточный тест», «Пробный ЕНТ»." /> : (
          <div className="divide-y divide-line border-t border-line">
            {c.types.map((t) => (
              <div key={t.id} className={cx('flex flex-wrap items-center gap-4 px-5 py-3.5', t.archived && 'opacity-60')}>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2 font-medium">{t.name}{t.archived && <Badge>Архив</Badge>}</div>
                  <div className="mt-0.5 text-[13px] text-muted">
                    {scoringLabel[t.scoring]} · {t.skills.map((s) => t.scoring === 'score' ? `${s.name} (${num(s.max_score)})` : s.name).join(', ')}
                    {t.results > 0 && ` · результатов: ${t.results}`}
                  </div>
                </div>
                <div className="flex items-center gap-1.5 text-[13px]" title="Показывать преподавателю в Telegram Mini App">
                  <Send className="size-3.5 text-sky-600" />
                  <Checkbox checked={t.show_in_miniapp} disabled={t.archived} onChange={(v) => patch.mutate({ id: t.id, body: { show_in_miniapp: v } })} label="В Mini App" />
                </div>
                <div className="flex gap-1">
                  {!t.archived && <Button size="sm" variant="ghost" onClick={() => setEdit(t)}>Изменить</Button>}
                  <Button size="sm" variant="ghost" onClick={() => patch.mutate({ id: t.id, body: { archived: !t.archived } })}>{t.archived ? 'Восстановить' : 'В архив'}</Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
      {edit && <TypeModal type={edit === 'new' ? null : edit} skills={activeSkills} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); refresh(); onToast('Тип теста сохранён') }} />}
    </>
  )
}

function TypeModal({ type, skills, onClose, onSaved }: { type: TestType | null; skills: TestSkill[]; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(type?.name ?? '')
  const [description, setDescription] = useState(type?.description ?? '')
  const [scoring, setScoring] = useState<'score' | 'grade'>(type?.scoring ?? 'score')
  const [show, setShow] = useState(type?.show_in_miniapp ?? true)
  // выбранные навыки в порядке добавления: skill_id → максимальный балл
  const [sel, setSel] = useState<{ skill_id: string; max: string }[]>(type?.skills.map((s) => ({ skill_id: s.skill_id, max: String(s.max_score) })) ?? [])
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: () => {
      const body = { name, description, scoring, show_in_miniapp: show, skills: sel.map((s) => ({ skill_id: s.skill_id, max_score: scoring === 'score' ? Number(s.max.replace(',', '.')) : 1 })) }
      return type ? api.patch(`/test-types/${type.id}`, body) : api.post('/test-types', body)
    },
    onSuccess: onSaved,
    onError: (e) => {
      const f = errorsByField(e)
      setError(f.name ? 'Укажите название' : f.skills ? 'Выберите хотя бы один навык' : f['skills.max_score'] ? 'Максимальный балл должен быть больше нуля' : (e as ApiError).message)
    },
  })
  const toggle = (id: string, on: boolean) => setSel(on ? [...sel, { skill_id: id, max: '100' }] : sel.filter((s) => s.skill_id !== id))
  // архивный навык, уже подключённый к типу, остаётся в списке
  const list = [...skills, ...(type?.skills.filter((s) => !skills.some((x) => x.id === s.skill_id)).map((s) => ({ id: s.skill_id, name: s.name, archived: true })) ?? [])]
  const locked = !!type && type.results > 0
  const total = sel.reduce((a, s) => a + (Number(s.max.replace(',', '.')) || 0), 0)
  return (
    <Modal open onClose={onClose} title={type ? 'Тип теста' : 'Новый тип теста'} wide
      footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" loading={save.isPending} disabled={!name.trim() || !sel.length} onClick={() => { setError(''); save.mutate() }}>Сохранить</Button></>}>
      <div className="space-y-4">
        <Field label="Название" required><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Промежуточный тест" maxLength={160} /></Field>
        <Field label="Описание" hint="Видно преподавателю при вводе результатов"><Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} /></Field>
        <Field label="Как оценивается навык" hint={locked ? 'По тесту уже есть результаты — способ оценки не меняется.' : scoring === 'score' ? 'Преподаватель ставит баллы; итог и процент считаются автоматически, оценка выставляется по порогам из вкладки «Оценки».' : 'Преподаватель выбирает оценку из шкалы для каждого навыка.'}>
          <div className={cx(locked && 'pointer-events-none opacity-60')}><Segmented value={scoring} onChange={setScoring} items={[{ value: 'score', label: 'Баллы' }, { value: 'grade', label: 'Оценки' }]} /></div>
        </Field>
        <div>
          <div className="mb-1.5 flex items-center justify-between text-[13px] font-medium"><span>Навыки <span className="text-red-600">*</span></span>{scoring === 'score' && sel.length > 0 && <span className="text-muted">Максимум за тест: <b className="text-ink tabular">{num(total)}</b></span>}</div>
          <div className="divide-y divide-line rounded-lg border border-line">
            {list.map((s) => {
              const i = sel.findIndex((x) => x.skill_id === s.id)
              return (
                <div key={s.id} className="flex items-center gap-3 px-3 py-2">
                  <div className="flex-1"><Checkbox checked={i >= 0} onChange={(v) => toggle(s.id, v)} label={<>{s.name}{s.archived && <span className="ml-1 text-xs text-muted">(архив)</span>}</>} /></div>
                  {i >= 0 && scoring === 'score' && (
                    <label className="flex items-center gap-2 text-[13px] text-muted">макс. балл
                      <Input className="!w-20 text-right" inputMode="decimal" value={sel[i].max} onChange={(e) => setSel(sel.map((x, j) => j === i ? { ...x, max: e.target.value } : x))} />
                    </label>
                  )}
                </div>
              )
            })}
          </div>
        </div>
        <div className="rounded-lg bg-sky-50 p-3"><Checkbox checked={show} onChange={setShow} label="Показывать преподавателям в Telegram Mini App" /></div>
        {error && <div className="text-sm text-red-700">{error}</div>}
      </div>
    </Modal>
  )
}

// ---------------------------------------------------------------- навыки

function Skills({ skills, onToast }: { skills: TestSkill[]; onToast: (s: string) => void }) {
  const refresh = useRefresh()
  const [name, setName] = useState('')
  const [edit, setEdit] = useState<{ id: string; name: string } | null>(null)
  const add = useMutation({ mutationFn: () => api.post('/test-skills', { name }), onSuccess: () => { setName(''); refresh() } })
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) => api.patch(`/test-skills/${id}`, body),
    onSuccess: () => { setEdit(null); refresh(); onToast('Сохранено') },
  })
  return (
    <Card title="Навыки" bodyClass="!px-0 !pb-0">
      <form className="flex gap-2 px-5 pb-4" onSubmit={(e) => { e.preventDefault(); if (name.trim()) add.mutate() }}>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Например: Grammar, Reading, Алгебра" maxLength={120} />
        <Button variant="primary" type="submit" loading={add.isPending} disabled={!name.trim()}>Добавить</Button>
      </form>
      {skills.length === 0 ? <Empty title="Навыков пока нет" text="Навыки — это то, что проверяет тест. Один навык можно использовать в нескольких типах тестов." /> : (
        <div className="divide-y divide-line border-t border-line">
          {skills.map((s) => (
            <div key={s.id} className={cx('flex items-center gap-3 px-5 py-2.5', s.archived && 'opacity-60')}>
              {edit?.id === s.id ? (
                <form className="flex flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); patch.mutate({ id: s.id, body: { name: edit.name } }) }}>
                  <Input autoFocus value={edit.name} onChange={(e) => setEdit({ ...edit, name: e.target.value })} maxLength={120} />
                  <Button type="submit" size="md" disabled={!edit.name.trim()}>Сохранить</Button>
                  <Button type="button" variant="ghost" onClick={() => setEdit(null)}>Отмена</Button>
                </form>
              ) : (
                <>
                  <div className="flex-1 text-sm">{s.name}{s.archived && <Badge className="ml-2">Архив</Badge>}</div>
                  {!s.archived && <Button size="sm" variant="ghost" onClick={() => setEdit({ id: s.id, name: s.name })}>Переименовать</Button>}
                  <Button size="sm" variant="ghost" onClick={() => patch.mutate({ id: s.id, body: { archived: !s.archived } })}>{s.archived ? 'Восстановить' : 'В архив'}</Button>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

// ---------------------------------------------------------------- оценки

type GradeForm = { id?: string; name: string; color: string; min: string }

function Grades({ grades, onToast }: { grades: TestGrade[]; onToast: (s: string) => void }) {
  const refresh = useRefresh()
  const [form, setForm] = useState<GradeForm | null>(null)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: (f: GradeForm) => {
      const min = f.min.trim() === '' ? null : Number(f.min.replace(',', '.'))
      const body = { name: f.name, color: f.color, min_percent: min, clear_min_percent: min == null }
      return f.id ? api.patch(`/test-grades/${f.id}`, body) : api.post('/test-grades', body)
    },
    onSuccess: () => { setForm(null); refresh(); onToast('Оценка сохранена') },
    onError: (e) => setError(errorsByField(e).min_percent ? 'Порог — число от 0 до 100' : (e as Error).message),
  })
  const archive = useMutation({ mutationFn: (g: TestGrade) => api.patch(`/test-grades/${g.id}`, { archived: !g.archived }), onSuccess: refresh })
  return (
    <>
      <Card title="Шкала оценок" bodyClass="!px-0 !pb-0" action={<Button variant="primary" size="sm" onClick={() => { setError(''); setForm({ name: '', color: palette[0], min: '' }) }}>+ Оценка</Button>}>
        <p className="px-5 pb-4 text-sm text-muted">Оценки используются в тестах «по оценкам», а для тестов «по баллам» выставляются автоматически: ученик получает оценку с наибольшим порогом, который он набрал. Без порога оценку можно поставить только вручную.</p>
        {grades.length === 0 ? <Empty title="Оценок пока нет" text="Например: «Отлично» от 85%, «Хорошо» от 70%, «Удовлетворительно» от 50% — или уровни A1…C2." /> : (
          <div className="divide-y divide-line border-t border-line">
            {grades.map((g) => (
              <div key={g.id} className={cx('flex items-center gap-3 px-5 py-2.5', g.archived && 'opacity-60')}>
                <span className="inline-flex min-w-16 justify-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-semibold" style={gradeStyle(g.color)}>{g.name}</span>
                <div className="flex-1 text-sm text-muted">{g.min_percent != null ? `от ${num(g.min_percent)}%` : 'только вручную'}{g.archived && ' · архив'}</div>
                {!g.archived && <Button size="sm" variant="ghost" onClick={() => { setError(''); setForm({ id: g.id, name: g.name, color: g.color ?? '', min: g.min_percent != null ? String(g.min_percent) : '' }) }}>Изменить</Button>}
                <Button size="sm" variant="ghost" onClick={() => archive.mutate(g)}>{g.archived ? 'Восстановить' : 'В архив'}</Button>
              </div>
            ))}
          </div>
        )}
      </Card>
      {form && (
        <Modal open onClose={() => setForm(null)} title={form.id ? 'Оценка' : 'Новая оценка'}
          footer={<><Button onClick={() => setForm(null)}>Отмена</Button><Button variant="primary" loading={save.isPending} disabled={!form.name.trim()} onClick={() => { setError(''); save.mutate(form) }}>Сохранить</Button></>}>
          <div className="space-y-4">
            <Field label="Название" required><Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Отлично, 5, B2…" maxLength={60} /></Field>
            <Field label="Порог, %" hint="Минимальный процент баллов для автоматической оценки. Пусто — оценка ставится только вручную.">
              <Input className="!w-32" inputMode="decimal" value={form.min} onChange={(e) => setForm({ ...form, min: e.target.value })} placeholder="85" />
            </Field>
            <Field label="Цвет">
              <div className="flex flex-wrap gap-2">
                {palette.map((p) => <button key={p} type="button" onClick={() => setForm({ ...form, color: p })} aria-label={p} className={cx('size-8 rounded-full border-2', form.color === p ? 'border-ink' : 'border-transparent')} style={{ backgroundColor: p }} />)}
              </div>
            </Field>
            {error && <div className="text-sm text-red-700">{error}</div>}
          </div>
        </Modal>
      )}
    </>
  )
}
