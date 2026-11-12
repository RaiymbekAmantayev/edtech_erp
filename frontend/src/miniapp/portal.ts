// Клиент кабинета ученика/родителя: свой токен (issuer emp-portal), хранится в sessionStorage вкладки Mini App.
import { ApiError } from '../lib/api'
import type { TestResult } from '../lib/tests'

const KEY = 'emp.portal'
let token: string | null = (() => { try { return sessionStorage.getItem(KEY) } catch { return null } })()

export const setPortalToken = (t: string | null) => {
  token = t
  try { if (t) sessionStorage.setItem(KEY, t); else sessionStorage.removeItem(KEY) } catch { /* приватный режим */ }
}
export const hasPortalToken = () => !!token

export async function portal<T = any>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch('/api/v1' + path, {
    method,
    headers: { ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await r.text()
  const json = text ? JSON.parse(text) : {}
  if (!r.ok) {
    const e = json.error ?? {}
    throw new ApiError(r.status, e.code ?? 'ERROR', e.message ?? 'Ошибка запроса', e.fields ?? [])
  }
  return json.data as T
}

// ---------------------------------------------------------------- Telegram WebApp

type TG = { initData: string; ready: () => void; expand: () => void; close: () => void }
declare global { interface Window { Telegram?: { WebApp?: TG } } }

// Скрипт Telegram подключается только в кабинете; вне Telegram initData пустой и вход идёт по ИИН.
export function loadTelegram(): Promise<TG | null> {
  if (window.Telegram?.WebApp) return Promise.resolve(window.Telegram.WebApp)
  return new Promise((resolve) => {
    const s = document.createElement('script')
    s.src = 'https://telegram.org/js/telegram-web-app.js'
    const done = () => {
      const tg = window.Telegram?.WebApp
      if (tg?.initData) { tg.ready(); tg.expand(); resolve(tg) } else resolve(null)
    }
    s.onload = done
    s.onerror = () => resolve(null)
    setTimeout(() => resolve(window.Telegram?.WebApp?.initData ? window.Telegram.WebApp : null), 4000)
    document.head.appendChild(s)
  })
}

// ---------------------------------------------------------------- типы

export type Lesson = { id: string; starts_at: string; ends_at: string; status: string; group: string; teacher: string | null; classroom: string | null; topic: string | null; meeting_url: string | null }
export type PortalMe = {
  kind: 'student' | 'parent'
  school: string
  telegram_linked: boolean
  profile: { id: string; full_name: string; phone: string | null; email: string | null; number?: number; branch?: string; birth_date?: string | null; notifications?: boolean }
  children: { id: string; full_name: string; relation?: string }[]
}
export type Overview = {
  profile: { id: string; full_name: string; number: number; status: string; branch: string; birth_date: string | null }
  balance_tiyn: number
  groups: { id: string; name: string; course: string; teacher: string | null }[]
  upcoming: Lesson[]
  attendance: { lesson_id: string; starts_at: string; group: string; status: string; topic: string | null; homework: string | null; teacher: string | null }[]
  attendance_90d: Record<string, number>
  tests: TestResult[]
  subscriptions: { name: string; type: string; lessons_total: number | null; lessons_used: number; lessons_left?: number; starts_on: string; ends_on: string | null; status: string }[]
}
export type TeacherCabinet = {
  profile: { id: string; full_name: string; phone: string | null; email: string | null; position: string | null; subjects: string[]; hired_at: string | null; branches: string[]; school: string }
  stats: { lessons_month: number; lessons_total: number; unmarked: number; groups: number; students: number; attendance_rate_30d: number }
  schedule: Lesson[]
  to_mark: Lesson[]
  conducted: { id: string; starts_at: string; group: string; topic: string | null; marked: number; present: number }[]
}
