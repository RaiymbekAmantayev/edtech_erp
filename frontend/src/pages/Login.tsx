import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { ApiError } from '../lib/api'
import { setLang, useLang, type Lang, tr } from '../lib/i18n'
import { Button, Field, Input, cx } from '../components/ui'
import { Logo } from '../components/Layout'

export function AuthShell({ children, aside }: { children: React.ReactNode; aside?: React.ReactNode }) {
  const [lang, t] = useLang()
  return (
    <div className="grid min-h-full lg:grid-cols-[minmax(0,560px)_1fr]">
      <div className="hidden flex-col bg-side p-12 lg:flex">
        <Logo />
        <h1 className="mt-14 text-[32px] font-bold leading-tight text-white">{t('auth.title', 'Учебный центр в одной системе')}</h1>
        <p className="mt-3 text-[15px] leading-relaxed text-gray-300">{t('auth.subtitle', 'Ученики, расписание, посещаемость, оплаты и уведомления родителям в Telegram.')}</p>
        <div className="mt-10 flex-1">{aside}</div>
        <div className="rounded-xl bg-white/5 p-5">
          <div className="text-sm font-semibold text-white">{tr('14 дней бесплатно на тарифе Business')}</div>
          <div className="mt-1 text-sm text-gray-400">{tr('Карта не нужна. Данные хранятся на серверах в Казахстане.')}</div>
        </div>
      </div>
      <div className="flex flex-col bg-white px-6 py-10 sm:px-14">
        <div className="flex justify-end gap-1.5">
          {(['ru', 'kk', 'en'] as Lang[]).map((l) => (
            <button key={l} onClick={() => setLang(l)} className={cx('rounded-md border px-3 py-1.5 text-xs font-semibold', lang === l ? 'border-ink bg-ink text-white' : 'border-line text-ink')}>
              {l === 'kk' ? tr('ҚАЗ') : l.toUpperCase()}
            </button>
          ))}
        </div>
        <div className="mx-auto flex w-full max-w-xl flex-1 flex-col justify-center py-8">{children}</div>
      </div>
    </div>
  )
}

export default function Login() {
  const { login } = useAuth()
  const [, t] = useLang()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await login(email, password)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : tr('Ошибка входа'))
    } finally {
      setBusy(false)
    }
  }
  const demo = (e: string) => { setEmail(e); setPassword('Demo12345!') }
  return (
    <AuthShell aside={
      <div className="space-y-2 text-sm text-gray-300">
        <div className="font-semibold text-white">{tr('Демо-доступы (пароль Demo12345!)')}</div>
        {[['aigerim@bilimplus.kz', tr('Владелец центра')], ['erlan@bilimplus.kz', tr('Преподаватель')], ['madi@bilimplus.kz', tr('Менеджер продаж')], ['buh@bilimplus.kz', tr('Бухгалтер')], ['admin@emp.kz', tr('Super Admin платформы')]].map(([e, r]) => (
          <button key={e} onClick={() => demo(e)} className="flex w-full justify-between rounded-lg bg-white/5 px-3 py-2 text-left hover:bg-white/10">
            <span>{e}</span><span className="text-gray-500">{r}</span>
          </button>
        ))}
      </div>
    }>
      <h2 className="text-[26px] font-bold">{t('auth.login', 'Вход в систему')}</h2>
      <p className="mt-2 text-sm text-muted">{tr('Нет аккаунта?')} <Link to="/register" className="font-medium text-brand-600 hover:underline">{tr('Зарегистрировать центр')}</Link></p>
      <form onSubmit={submit} className="mt-8 space-y-4">
        <Field label={t('auth.email', 'Рабочий email')}><Input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@center.kz" required /></Field>
        <Field label={t('auth.password', 'Пароль')}><Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required /></Field>
        {error && <div className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-800">{error}</div>}
        <Button variant="primary" className="h-12 w-full text-[15px]" loading={busy}>{t('auth.login', 'Войти')}</Button>
      </form>
    </AuthShell>
  )
}
