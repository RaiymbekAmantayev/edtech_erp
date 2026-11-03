// HTTP-клиент: access-токен в памяти, refresh-токен — в httpOnly-cookie (обновляется автоматически при 401).
export type FieldError = { field: string; code: string }
export class ApiError extends Error {
  status: number
  code: string
  fields: FieldError[]
  meta?: Record<string, unknown>
  constructor(status: number, code: string, message: string, fields: FieldError[] = [], meta?: Record<string, unknown>) {
    super(message)
    this.status = status
    this.code = code
    this.fields = fields
    this.meta = meta
  }
}

let accessToken: string | null = null
let refreshing: Promise<boolean> | null = null
const listeners = new Set<() => void>()

export const setToken = (t: string | null) => { accessToken = t }
export const hasToken = () => !!accessToken
export const onAuthLost = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn) } }

export async function refresh(): Promise<boolean> {
  if (!refreshing) {
    refreshing = fetch('/api/v1/auth/refresh', { method: 'POST', credentials: 'same-origin' })
      .then(async (r) => {
        if (!r.ok) return false
        const j = await r.json()
        accessToken = j.data.access_token
        return true
      })
      .catch(() => false)
      .finally(() => setTimeout(() => { refreshing = null }, 0))
  }
  return refreshing
}

type Opts = { headers?: Record<string, string>; raw?: boolean; form?: FormData }

export async function request<T = unknown>(method: string, path: string, body?: unknown, opts: Opts = {}, retry = true): Promise<{ data: T; meta: Record<string, unknown> }> {
  const headers: Record<string, string> = { ...opts.headers }
  if (accessToken) headers.Authorization = `Bearer ${accessToken}`
  if (body !== undefined && !opts.form) headers['Content-Type'] = 'application/json'
  const r = await fetch('/api/v1' + path, {
    method,
    headers,
    credentials: 'same-origin',
    body: opts.form ?? (body !== undefined ? JSON.stringify(body) : undefined),
  })
  if (r.status === 401 && retry && !path.startsWith('/auth/')) {
    if (await refresh()) return request<T>(method, path, body, opts, false)
    listeners.forEach((l) => l())
  }
  const text = await r.text()
  const json = text ? JSON.parse(text) : {}
  if (!r.ok) {
    const e = json.error ?? {}
    throw new ApiError(r.status, e.code ?? 'ERROR', e.message ?? 'Ошибка запроса', e.fields ?? [])
  }
  // конфликт расписания приходит как 200 с requires_force
  if (json.meta?.requires_force) throw new ApiError(409, 'SCHEDULE_CONFLICT', 'Конфликт расписания', [], json.meta)
  return { data: json.data as T, meta: json.meta ?? {} }
}

export const api = {
  get: <T = any>(p: string) => request<T>('GET', p).then((r) => r.data),
  getMeta: <T = any>(p: string) => request<T>('GET', p),
  post: <T = any>(p: string, b?: unknown, h?: Record<string, string>) => request<T>('POST', p, b ?? {}, { headers: h }).then((r) => r.data),
  patch: <T = any>(p: string, b: unknown) => request<T>('PATCH', p, b).then((r) => r.data),
  put: <T = any>(p: string, b: unknown) => request<T>('PUT', p, b).then((r) => r.data),
  del: <T = any>(p: string) => request<T>('DELETE', p).then((r) => r.data),
  upload: <T = any>(p: string, form: FormData) => request<T>('POST', p, undefined, { form }).then((r) => r.data),
}

// Скачивание файла (экспорт) с авторизацией.
export async function download(path: string) {
  const go = () => fetch('/api/v1' + path, { headers: accessToken ? { Authorization: `Bearer ${accessToken}` } : {} })
  let r = await go()
  if (r.status === 401 && (await refresh())) r = await go()
  if (!r.ok) throw new ApiError(r.status, 'EXPORT_FAILED', 'Не удалось выгрузить файл')
  const blob = await r.blob()
  const name = /filename="([^"]+)"/.exec(r.headers.get('Content-Disposition') ?? '')?.[1] ?? 'export'
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = name
  a.click()
  URL.revokeObjectURL(a.href)
}

const codes: Record<string, string> = {
  REQUIRED: 'Обязательное поле', INVALID_FORMAT: 'Неверный формат', INVALID_IIN: 'Неверный ИИН', TOO_SHORT: 'Слишком коротко',
  TOO_LONG: 'Слишком длинно', TOO_SMALL: 'Меньше допустимого', TOO_LARGE: 'Больше допустимого', INVALID_NUMBER: 'Введите число',
  INVALID_DATE: 'Неверная дата', INVALID_OPTION: 'Выберите вариант из списка', INVALID_URL: 'Ссылка должна начинаться с http(s)://',
  NOT_UNIQUE: 'Значение уже используется', CUSTOM_FIELD_READ_ONLY: 'Нет прав на изменение', INVALID_TIME: 'Неверное время',
  EXCEEDS_PAYMENT: 'Больше суммы платежа', INVALID: 'Неверное значение', IN_FUTURE: 'Дата в будущем', INVALID_MONEY: 'Неверная сумма',
}
export const fieldErrorText = (code: string) => codes[code] ?? code

export function errorsByField(e: unknown): Record<string, string> {
  const out: Record<string, string> = {}
  if (e instanceof ApiError) for (const f of e.fields) out[f.field] = fieldErrorText(f.code)
  return out
}
