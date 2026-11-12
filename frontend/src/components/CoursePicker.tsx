import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { Checkbox } from './ui'

type Course = { id: string; name: string; status: string }

export const useCourses = () => useQuery({ queryKey: ['courses', 'all'], queryFn: () => api.get<Course[]>('/courses?all=1') })

// Выбор курсов: пустой список означает «для всех курсов».
export function CoursePicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const q = useCourses()
  const courses = (q.data ?? []).filter((c) => c.status === 'active' || value.includes(c.id))
  return (
    <div className="rounded-lg border border-line">
      <div className="border-b border-line px-3 py-2"><Checkbox checked={value.length === 0} onChange={(v) => v && onChange([])} label={<b className="font-medium">Все курсы</b>} /></div>
      <div className="grid max-h-48 gap-2 overflow-y-auto px-3 py-2 sm:grid-cols-2">
        {courses.map((c) => (
          <Checkbox key={c.id} checked={value.includes(c.id)} label={c.name}
            onChange={(v) => onChange(v ? [...value, c.id] : value.filter((x) => x !== c.id))} />
        ))}
        {q.data && courses.length === 0 && <span className="text-sm text-muted">Курсов пока нет</span>}
      </div>
    </div>
  )
}

export function courseNames(ids: string[], all: Course[] | undefined) {
  if (!ids.length) return 'все курсы'
  return ids.map((id) => all?.find((c) => c.id === id)?.name ?? '…').join(', ')
}
