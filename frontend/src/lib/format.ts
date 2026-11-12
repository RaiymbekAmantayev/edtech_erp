import { tr, intlLocale } from './i18n'
// Форматирование по локали пользователя: 02.11.2026, 25 000 ₸. Деньги приходят в тиын (целые числа) — без float-арифметики.
export const TZ = 'Asia/Almaty'

export function money(tiyn: number | null | undefined, opts: { sign?: boolean } = {}): string {
  if (tiyn == null) return '—'
  const neg = tiyn < 0
  const whole = Math.trunc(Math.abs(tiyn) / 100)
  const s = whole.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ' ') + ' ₸'
  if (neg) return '−' + s
  return opts.sign && tiyn > 0 ? '+' + s : s
}

// «18 000» / «18000,50» → тиын (целое).
export function parseMoney(input: string): number | null {
  const s = input.replace(/[\s ₸]/g, '').replace(',', '.')
  if (!/^-?\d+(\.\d{0,2})?$/.test(s)) return null
  const [w, f = ''] = s.replace('-', '').split('.')
  const v = Number(w) * 100 + Number((f + '00').slice(0, 2))
  return s.startsWith('-') ? -v : v
}

export function phone(e164?: string | null): string {
  if (!e164) return ''
  const m = /^\+7(\d{3})(\d{3})(\d{2})(\d{2})$/.exec(e164)
  return m ? `+7 ${m[1]} ${m[2]}-${m[3]}-${m[4]}` : e164
}

const fmt = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(intlLocale(), { timeZone: TZ, ...o })
export const date = (v?: string | Date | null) => (v ? fmt({ day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date(v)) : '—')
export const dateShort = (v?: string | Date | null) => (v ? fmt({ day: '2-digit', month: '2-digit' }).format(new Date(v)) : '—')
export const time = (v?: string | Date | null) => (v ? fmt({ hour: '2-digit', minute: '2-digit' }).format(new Date(v)) : '')
export const dateTime = (v?: string | Date | null) => (v ? `${dateShort(v)}, ${time(v)}` : '—')
export const longDay = (v: Date) => fmt({ weekday: 'long', day: 'numeric', month: 'long' }).format(v)
export const dayMonth = (v: Date) => fmt({ weekday: 'short', day: 'numeric', month: 'long' }).format(v)

// Дата в часовом поясе центра в формате YYYY-MM-DD.
export function isoDay(d: Date = new Date()): string {
  const p = new Intl.DateTimeFormat('en-CA', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit' }).format(d)
  return p
}

// Начало суток в часовом поясе центра (Казахстан — UTC+5 с 01.03.2024).
export function dayStart(day: string): Date {
  return new Date(`${day}T00:00:00+05:00`)
}

export function addDays(day: string, n: number): string {
  const d = new Date(`${day}T12:00:00+05:00`)
  d.setUTCDate(d.getUTCDate() + n)
  return isoDay(d)
}

export function relative(v?: string | null): string {
  if (!v) return tr('никогда')
  const diff = Date.now() - new Date(v).getTime()
  const days = Math.floor(diff / 864e5)
  if (days <= 0) return tr('сегодня, {0}', time(v))
  if (days === 1) return tr('вчера, {0}', time(v))
  return tr('{0} {1} назад', days, plural(days, tr('день'), tr('дня'), tr('дней')))
}

export function plural(n: number, one: string, few: string, many: string) {
  const m10 = n % 10, m100 = n % 100
  if (m10 === 1 && m100 !== 11) return one
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few
  return many
}

export function age(birth?: string | null): string {
  if (!birth) return ''
  const b = new Date(birth)
  const n = new Date()
  let a = n.getFullYear() - b.getFullYear()
  if (n.getMonth() < b.getMonth() || (n.getMonth() === b.getMonth() && n.getDate() < b.getDate())) a--
  return `${a} ${plural(a, tr('год'), tr('года'), tr('лет'))}`
}

export const initials = (name: string) => name.split(/\s+/).filter(Boolean).slice(0, 2).map((p) => p[0]).join('').toUpperCase()

export const weekdayShort = [tr('Пн'), tr('Вт'), tr('Ср'), tr('Чт'), tr('Пт'), tr('Сб'), tr('Вс')]
export const weekdaysText = (w: number[]) => w.map((d) => weekdayShort[d - 1]).join(', ')
