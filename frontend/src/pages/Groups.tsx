import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { api, errorsByField } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, parseMoney, weekdaysText } from '../lib/format'
import type { Classroom, Course, Employee, Group } from '../lib/types'
import { Button, Card, Field, Input, Modal, Select, Spinner, Table, Tabs, Textarea, cx } from '../components/ui'
import { tr } from '../lib/i18n'

export default function Groups() {
  const { can, branchId } = useAuth()
  const nav = useNavigate()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as 'groups' | 'individual' | 'courses') ?? 'groups'
  const [modal, setModal] = useState<null | 'group' | 'individual' | 'course' | Course>(params.get('new') === 'group' ? 'group' : null)
  const [q, setQ] = useState('')
  const groups = useQuery({ queryKey: ['groups', tab, branchId, q], queryFn: () => api.get<Group[]>(`/groups?type=${tab === 'individual' ? 'individual' : 'group'}&branch_id=${branchId}&q=${encodeURIComponent(q)}`), enabled: tab !== 'courses' })
  const courses = useQuery({ queryKey: ['courses', 'all'], queryFn: () => api.get<Course[]>('/courses?all=1') })

  return (
    <div className="mx-auto max-w-[1400px] p-4 lg:p-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-[26px] font-bold">{tr('Группы и курсы')}</h1>
        {can('groups.edit') && (
          <div className="flex gap-2">
            <Button onClick={() => setModal('course')}>{tr('+ Курс')}</Button>
            <Button onClick={() => setModal('individual')}>{tr('+ Индивидуально')}</Button>
            <Button variant="primary" onClick={() => setModal('group')}><Plus className="size-4" />{tr('Группа')}</Button>
          </div>
        )}
      </div>
      <Tabs className="mt-5" value={tab} onChange={(v) => setParams({ tab: v })} items={[{ value: 'groups', label: tr('Группы') }, { value: 'individual', label: tr('Индивидуальные') }, { value: 'courses', label: tr('Курсы'), count: courses.data?.length }]} />

      {tab !== 'courses' && (
        <>
          <Input className="mt-4 !h-9 max-w-xs" placeholder={tr('Поиск группы или курса')} value={q} onChange={(e) => setQ(e.target.value)} />
          <Card className="mt-3" bodyClass="!p-0">
            {groups.isLoading ? <Spinner /> : (
              <Table head={[tr('Группа'), tr('Курс'), tr('Преподаватель'), tr('Расписание'), tr('Кабинет'), tr('Состав'), tr('Филиал')]}>
                {groups.data?.map((g) => (
                  <tr key={g.id} className="cursor-pointer hover:bg-gray-50" onClick={() => nav('/groups/' + g.id)}>
                    <td className="px-4 py-3 font-medium">{g.name}</td>
                    <td className="px-4 py-3 text-[13px]">{g.course_name}</td>
                    <td className="px-4 py-3 text-[13px]">{g.teacher_name ?? '—'}</td>
                    <td className="px-4 py-3 text-[13px]">{g.schedule.map((s) => `${weekdaysText(s.weekdays)} ${s.start_time}`).join('; ') || <span className="text-muted">{tr('не задано')}</span>}</td>
                    <td className="px-4 py-3 text-[13px]">{g.classroom_name ?? '—'}</td>
                    <td className="px-4 py-3"><Fill n={g.students} cap={g.capacity} />{g.waitlist > 0 && <span className="ml-2 text-xs text-muted">+{g.waitlist} {tr('в ожидании')}</span>}</td>
                    <td className="px-4 py-3 text-[13px] text-muted">{g.branch_name}</td>
                  </tr>
                ))}
              </Table>
            )}
            {groups.data?.length === 0 && <div className="py-12 text-center text-sm text-muted">{tr('Групп пока нет')}</div>}
          </Card>
        </>
      )}
      {tab === 'courses' && (
        <Card className="mt-4" bodyClass="!p-0">
          <Table head={[tr('Курс'), tr('Категория'), tr('Занятий'), tr('Длительность'), tr('Стоимость'), tr('Групп'), tr('Учеников'), tr('Статус')]}>
            {courses.data?.map((c) => (
              <tr key={c.id} className={cx('hover:bg-gray-50', can('groups.edit') && 'cursor-pointer')} onClick={() => can('groups.edit') && setModal(c)}>
                <td className="px-4 py-3 font-medium">{c.name}</td>
                <td className="px-4 py-3 text-[13px]">{c.category ?? '—'}</td>
                <td className="px-4 py-3 tabular">{c.lessons_count}</td>
                <td className="px-4 py-3 tabular">{c.duration_minutes} {tr('мин')}</td>
                <td className="px-4 py-3 font-semibold tabular">{money(c.price_tiyn)}</td>
                <td className="px-4 py-3 tabular">{c.groups}</td>
                <td className="px-4 py-3 tabular">{c.students}</td>
                <td className="px-4 py-3 text-[13px]">{c.status === 'active' ? tr('Активен') : tr('В архиве')}</td>
              </tr>
            ))}
          </Table>
        </Card>
      )}
      {(modal === 'group' || modal === 'individual') && <GroupModal individual={modal === 'individual'} onClose={() => { setModal(null); if (params.get('new')) setParams({}) }} onSaved={(id) => nav('/groups/' + id)} />}
      {modal && typeof modal !== 'string' && <CourseModal course={modal} onClose={() => setModal(null)} />}
      {modal === 'course' && <CourseModal onClose={() => setModal(null)} />}
    </div>
  )
}

export const Fill = ({ n, cap }: { n: number; cap: number | null }) => (
  <span className={cx('text-sm font-medium tabular', cap && n >= cap ? 'text-red-700' : '')}>{n}{cap ? ` / ${cap}` : ''}</span>
)

export function GroupModal({ group, individual, onClose, onSaved }: { group?: Group; individual?: boolean; onClose: () => void; onSaved: (id: string) => void }) {
  const { me, branchId } = useAuth()
  const qc = useQueryClient()
  const courses = useQuery({ queryKey: ['courses'], queryFn: () => api.get<Course[]>('/courses') })
  const teachers = useQuery({ queryKey: ['employees', 'teachers'], queryFn: () => api.get<Employee[]>('/employees?teachers=1') })
  const rooms = useQuery({ queryKey: ['classrooms', 'all'], queryFn: () => api.get<Classroom[]>('/classrooms') })
  const students = useQuery({ queryKey: ['students', 'pick'], queryFn: () => api.get<{ id: string; full_name: string }[]>('/students?limit=200&status=active,registration'), enabled: !!individual })
  const [f, setF] = useState({
    name: group?.name ?? '', branch_id: group?.branch_id ?? (branchId || me?.branches?.[0]?.id || ''), course_id: group?.course_id ?? '', teacher_id: group?.teacher_id ?? '',
    classroom_id: group?.classroom_id ?? '', capacity: group?.capacity ? String(group.capacity) : '', start_date: group?.start_date?.slice(0, 10) ?? '', end_date: group?.end_date?.slice(0, 10) ?? '', student_id: '',
  })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({
    mutationFn: () => {
      const body = { ...f, capacity: f.capacity ? Number(f.capacity) : undefined, type: individual ? 'individual' : 'group' }
      return group ? api.patch(`/groups/${group.id}`, body).then(() => ({ id: group.id })) : api.post<{ id: string }>('/groups', body)
    },
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ['groups'] }); qc.invalidateQueries({ queryKey: ['group'] }); onSaved(r.id) },
    onError: (e) => setErrors(errorsByField(e)),
  })
  return (
    <Modal open title={group ? tr('Редактирование группы') : individual ? tr('Индивидуальные занятия') : tr('Новая группа')} onClose={onClose}
      footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        {individual ? (
          <Field label={tr('Ученик')} className="sm:col-span-2" error={errors.student_id}><Select value={f.student_id} onChange={(e) => setF({ ...f, student_id: e.target.value })}><option value="">{tr('Выберите ученика')}</option>{students.data?.map((s) => <option key={s.id} value={s.id}>{s.full_name}</option>)}</Select></Field>
        ) : <Field label={tr('Название')} className="sm:col-span-2" error={errors.name}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder={tr('ЕНТ Математика 11А')} /></Field>}
        {!group && <Field label={tr('Филиал')} error={errors.branch_id}><Select value={f.branch_id} onChange={(e) => setF({ ...f, branch_id: e.target.value })}>{me?.branches?.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}</Select></Field>}
        {!group && <Field label={tr('Курс')} error={errors.course_id}><Select value={f.course_id} onChange={(e) => setF({ ...f, course_id: e.target.value })}><option value="">{tr('Выберите')}</option>{courses.data?.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</Select></Field>}
        <Field label={tr('Преподаватель')}><Select value={f.teacher_id} onChange={(e) => setF({ ...f, teacher_id: e.target.value })}><option value="">—</option>{teachers.data?.map((t) => <option key={t.id} value={t.id}>{t.full_name}</option>)}</Select></Field>
        <Field label={tr('Кабинет')}><Select value={f.classroom_id} onChange={(e) => setF({ ...f, classroom_id: e.target.value })}><option value="">—</option>{rooms.data?.filter((r) => r.branch_id === f.branch_id).map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}</Select></Field>
        {!individual && <Field label={tr('Лимит учеников')}><Input type="number" value={f.capacity} onChange={(e) => setF({ ...f, capacity: e.target.value })} /></Field>}
        <Field label={tr('Дата начала')}><Input type="date" value={f.start_date} onChange={(e) => setF({ ...f, start_date: e.target.value })} /></Field>
        <Field label={tr('Дата окончания')}><Input type="date" value={f.end_date} onChange={(e) => setF({ ...f, end_date: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

function CourseModal({ course, onClose }: { course?: Course; onClose: () => void }) {
  const qc = useQueryClient()
  const [f, setF] = useState({ name: course?.name ?? '', category: course?.category ?? '', description: course?.description ?? '', lessons_count: String(course?.lessons_count ?? 12), duration_minutes: String(course?.duration_minutes ?? 90), price: course ? String(course.price_tiyn / 100) : '', status: course?.status ?? 'active' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const m = useMutation({
    mutationFn: () => {
      const body = { ...f, lessons_count: Number(f.lessons_count), duration_minutes: Number(f.duration_minutes), price_tiyn: parseMoney(f.price) ?? 0 }
      return course ? api.patch(`/courses/${course.id}`, body) : api.post('/courses', body)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['courses'] }); onClose() },
    onError: (e) => setErrors(errorsByField(e)),
  })
  return (
    <Modal open title={course ? course.name : tr('Новый курс')} onClose={onClose} footer={<><Button onClick={onClose}>{tr('Отмена')}</Button><Button variant="primary" loading={m.isPending} onClick={() => m.mutate()}>{tr('Сохранить')}</Button></>}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={tr('Название')} className="sm:col-span-2" error={errors.name}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label={tr('Категория')}><Input value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })} placeholder={tr('ЕНТ, Английский, IT')} /></Field>
        <Field label={tr('Стоимость курса/абонемента, ₸')} error={errors.price_tiyn}><Input value={f.price} onChange={(e) => setF({ ...f, price: e.target.value })} /></Field>
        <Field label={tr('Количество занятий')}><Input type="number" value={f.lessons_count} onChange={(e) => setF({ ...f, lessons_count: e.target.value })} /></Field>
        <Field label={tr('Длительность, мин')}><Input type="number" value={f.duration_minutes} onChange={(e) => setF({ ...f, duration_minutes: e.target.value })} /></Field>
        <Field label={tr('Описание / программа')} className="sm:col-span-2"><Textarea value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
        {course && <Field label={tr('Статус')}><Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value })}><option value="active">{tr('Активен')}</option><option value="archived">{tr('В архиве')}</option></Select></Field>}
      </div>
    </Modal>
  )
}
