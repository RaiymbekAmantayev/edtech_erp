import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Search } from 'lucide-react'
import { api } from '../lib/api'
import { phone } from '../lib/format'
import { Badge, cx, leadStages, studentStatus } from './ui'

type Hit = { kind: string; id: string; title: string; subtitle: string; status: string }
const kinds: Record<string, string> = { student: 'Ученик', parent: 'Родитель', group: 'Группа', lead: 'Лид' }

// Глобальный поиск Ctrl/Cmd+K: ученики, родители, группы, лиды — по ФИО и телефону (с опечатками — pg_trgm).
export function SearchPalette({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState('')
  const [debounced, setDebounced] = useState('')
  const [idx, setIdx] = useState(0)
  const nav = useNavigate()
  useEffect(() => { const t = setTimeout(() => setDebounced(q), 200); return () => clearTimeout(t) }, [q])
  const res = useQuery({ queryKey: ['search', debounced], queryFn: () => api.get<Hit[]>('/search?q=' + encodeURIComponent(debounced)), enabled: debounced.trim().length >= 2 })
  const hits = res.data ?? []
  const open = (h: Hit) => {
    onClose()
    nav({ student: `/students/${h.id}`, parent: `/parents?open=${h.id}`, group: `/groups/${h.id}`, lead: `/leads?open=${h.id}` }[h.kind] ?? '/')
  }
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-gray-900/40 p-4 pt-[12vh]" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="w-full max-w-xl overflow-hidden rounded-2xl bg-white shadow-2xl">
        <div className="flex items-center gap-3 border-b border-line px-4">
          <Search className="size-5 text-muted" />
          <input autoFocus value={q} onChange={(e) => { setQ(e.target.value); setIdx(0) }} placeholder="ФИО, телефон или название группы"
            className="h-14 flex-1 bg-transparent text-[15px] outline-none"
            onKeyDown={(e) => {
              if (e.key === 'Escape') onClose()
              if (e.key === 'ArrowDown') { e.preventDefault(); setIdx(Math.min(idx + 1, hits.length - 1)) }
              if (e.key === 'ArrowUp') { e.preventDefault(); setIdx(Math.max(idx - 1, 0)) }
              if (e.key === 'Enter' && hits[idx]) open(hits[idx])
            }} />
        </div>
        <div className="max-h-96 overflow-y-auto py-2">
          {debounced.length < 2 && <div className="px-4 py-6 text-center text-sm text-muted">Начните вводить — например, «Алимжан» или последние цифры телефона</div>}
          {debounced.length >= 2 && !res.isFetching && hits.length === 0 && <div className="px-4 py-6 text-center text-sm text-muted">Ничего не найдено</div>}
          {hits.map((h, i) => (
            <button key={h.kind + h.id} onMouseEnter={() => setIdx(i)} onClick={() => open(h)}
              className={cx('flex w-full items-center gap-3 px-4 py-2.5 text-left', i === idx && 'bg-brand-50')}>
              <span className="w-20 shrink-0 text-xs font-medium text-muted">{kinds[h.kind]}</span>
              <span className="flex-1">
                <span className="block text-sm font-medium">{h.title}</span>
                {h.subtitle && <span className="block text-xs text-muted">{phone(h.subtitle)}</span>}
              </span>
              {h.kind === 'student' && studentStatus[h.status] && <Badge tone={studentStatus[h.status][1]}>{studentStatus[h.status][0]}</Badge>}
              {h.kind === 'lead' && leadStages[h.status] && <Badge tone={leadStages[h.status][1]}>{leadStages[h.status][0]}</Badge>}
              {h.kind === 'group' && h.status === 'individual' && <Badge>Индивидуально</Badge>}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
