import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Send, Check } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { ApiError, errorsByField, request } from '../lib/api'
import { Button, Checkbox, Field, Input, Select, cx } from '../components/ui'
import { AuthShell } from './Login'

const steps = [
  ['Аккаунт', 'владелец центра'],
  ['Центр', 'тип, город, размер'],
  ['Настройки', 'поля и язык'],
  ['Готово', 'первые шаги'],
]

const templates: Record<string, string> = {
  language: 'Языковая школа — уровень по тесту, цель обучения, целевой балл IELTS',
  ent: 'Подготовка к ЕНТ — класс, школа, целевой балл, профильные предметы, договор',
  kids: 'Детский центр — кто забирает ребёнка, аллергии (чувствительное поле)',
  it: 'IT-школа — уровень, свой ноутбук, GitHub',
  other: 'Без шаблона — поля можно добавить позже в «Настройки → Поля»',
}

// Онбординг (ТЗ 29.2, макет «Создайте аккаунт владельца»): 4 шага, создание организации с шаблоном полей отрасли.
export default function Register() {
  const { loginWithToken } = useAuth()
  const nav = useNavigate()
  const [step, setStep] = useState(0)
  const [f, setF] = useState({ full_name: '', phone: '', email: '', password: '', accept_terms: false, center_name: '', center_type: 'ent', city: 'Алматы', size: 'small', branch_name: '', address: '', template: '', locale: 'ru' })
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f) => (v: string | boolean) => setF({ ...f, [k]: v })

  const next = () => {
    const e: Record<string, string> = {}
    if (step === 0) {
      if (!f.full_name.trim()) e.full_name = 'Укажите имя и фамилию'
      if (f.phone.replace(/\D/g, '').length < 10) e.phone = 'Укажите телефон'
      if (!/^\S+@\S+\.\S+$/.test(f.email)) e.email = 'Неверный email'
      if (f.password.length < 10) e.password = 'Минимум 10 символов'
      if (!f.accept_terms) e.accept_terms = 'Нужно согласие'
    }
    if (step === 1 && !f.center_name.trim()) e.center_name = 'Укажите название центра'
    setErrors(e)
    if (Object.keys(e).length === 0) setStep(step + 1)
  }

  const submit = async () => {
    setBusy(true)
    try {
      const r = await request<{ access_token: string }>('POST', '/auth/register', { ...f, template: f.template || f.center_type })
      await loginWithToken(r.data.access_token)
      setStep(3)
    } catch (err) {
      const fe = errorsByField(err)
      setErrors(fe)
      if (err instanceof ApiError && err.code === 'EMAIL_TAKEN') { setErrors({ email: err.message }); setStep(0) }
      else if (fe.email || fe.phone || fe.password || fe.full_name) setStep(0)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthShell aside={
      <ol className="space-y-5">
        {steps.map(([t, s], i) => (
          <li key={t} className="flex items-start gap-4">
            <span className={cx('flex size-8 shrink-0 items-center justify-center rounded-full border text-sm font-semibold',
              i === step ? 'border-white bg-white text-side' : i < step ? 'border-brand-500 bg-brand-600 text-white' : 'border-gray-600 text-gray-400')}>
              {i < step ? <Check className="size-4" /> : i + 1}
            </span>
            <span><span className={cx('block font-semibold', i === step ? 'text-white' : 'text-gray-400')}>{t}</span><span className="text-sm text-gray-500">{s}</span></span>
          </li>
        ))}
      </ol>
    }>
      <div className="text-[13px] text-muted">Шаг {step + 1} из 4</div>
      {step === 0 && (
        <>
          <h2 className="mt-2 text-[26px] font-bold">Создайте аккаунт владельца</h2>
          <p className="mt-2 text-sm text-muted">Уже есть аккаунт? <Link to="/login" className="font-medium text-brand-600 hover:underline">Войти</Link></p>
          <button type="button" disabled title="Вход через Telegram заработает после настройки бота (TELEGRAM_BOT_TOKEN)"
            className="mt-7 flex h-12 w-full items-center justify-center gap-2 rounded-lg border border-line text-[15px] font-semibold text-ink opacity-70">
            <Send className="size-5 text-sky-500" />Продолжить через Telegram
          </button>
          <div className="my-6 flex items-center gap-3 text-[13px] text-muted"><span className="h-px flex-1 bg-line" />или по email<span className="h-px flex-1 bg-line" /></div>
          <div className="grid gap-4 sm:grid-cols-[1.4fr_1fr]">
            <Field label="Имя и фамилия" error={errors.full_name}><Input value={f.full_name} onChange={(e) => set('full_name')(e.target.value)} placeholder="Айгерим Касымова" /></Field>
            <Field label="Телефон" error={errors.phone}><Input type="tel" value={f.phone} onChange={(e) => set('phone')(e.target.value)} placeholder="+7 7__ ___-__-__" /></Field>
          </div>
          <Field className="mt-4" label="Рабочий email" error={errors.email}><Input type="email" value={f.email} onChange={(e) => set('email')(e.target.value)} placeholder="you@center.kz" /></Field>
          <Field className="mt-4" label="Пароль" error={errors.password} hint="На email придёт ссылка для подтверждения, на телефон — SMS-код.">
            <Input type="password" value={f.password} onChange={(e) => set('password')(e.target.value)} placeholder="Минимум 10 символов" />
          </Field>
          <div className="mt-4">
            <Checkbox checked={f.accept_terms} onChange={set('accept_terms')} label={<span>Принимаю <a className="text-brand-600" href="#">договор-оферту</a> и <a className="text-brand-600" href="#">политику обработки персональных данных</a></span>} />
            {errors.accept_terms && <div className="mt-1 text-xs text-red-600">{errors.accept_terms}</div>}
          </div>
          <Button variant="primary" className="mt-6 h-12 w-full text-[15px]" onClick={next}>Продолжить</Button>
        </>
      )}
      {step === 1 && (
        <>
          <h2 className="mt-2 text-[26px] font-bold">Расскажите о центре</h2>
          <div className="mt-7 space-y-4">
            <Field label="Название центра" error={errors.center_name}><Input value={f.center_name} onChange={(e) => set('center_name')(e.target.value)} placeholder="Например, Bilim Plus" /></Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Тип центра">
                <Select value={f.center_type} onChange={(e) => set('center_type')(e.target.value)}>
                  <option value="ent">Подготовка к ЕНТ</option><option value="language">Языковая школа</option><option value="kids">Детский центр</option>
                  <option value="it">Школа программирования</option><option value="other">Другое</option>
                </Select>
              </Field>
              <Field label="Город">
                <Select value={f.city} onChange={(e) => set('city')(e.target.value)}>
                  {['Алматы', 'Астана', 'Шымкент', 'Караганда', 'Актобе', 'Тараз', 'Павлодар', 'Усть-Каменогорск', 'Атырау', 'Костанай'].map((c) => <option key={c}>{c}</option>)}
                </Select>
              </Field>
            </div>
            <Field label="Размер">
              <div className="grid grid-cols-3 gap-2">
                {[['small', 'до 100 учеников'], ['medium', '100–500'], ['large', 'больше 500']].map(([v, l]) => (
                  <button key={v} type="button" onClick={() => set('size')(v)} className={cx('h-11 rounded-lg border text-sm font-medium', f.size === v ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line')}>{l}</button>
                ))}
              </div>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Первый филиал"><Input value={f.branch_name} onChange={(e) => set('branch_name')(e.target.value)} placeholder={`${f.city}, основной`} /></Field>
              <Field label="Адрес"><Input value={f.address} onChange={(e) => set('address')(e.target.value)} placeholder="пр. Абая, 52" /></Field>
            </div>
          </div>
          <div className="mt-6 flex gap-3"><Button className="h-12 flex-1" onClick={() => setStep(0)}>Назад</Button><Button variant="primary" className="h-12 flex-[2]" onClick={next}>Продолжить</Button></div>
        </>
      )}
      {step === 2 && (
        <>
          <h2 className="mt-2 text-[26px] font-bold">Поля и язык</h2>
          <p className="mt-2 text-sm text-muted">Подключим набор пользовательских полей вашей отрасли. Их можно изменить в любой момент.</p>
          <div className="mt-6 space-y-2">
            {Object.entries(templates).map(([k, v]) => (
              <button key={k} type="button" onClick={() => set('template')(k)}
                className={cx('w-full rounded-xl border p-4 text-left text-sm', (f.template || f.center_type) === k ? 'border-brand-500 bg-brand-50' : 'border-line hover:bg-gray-50')}>
                {v}
              </button>
            ))}
          </div>
          <Field className="mt-5" label="Язык интерфейса и уведомлений по умолчанию">
            <Select value={f.locale} onChange={(e) => set('locale')(e.target.value)}><option value="ru">Русский</option><option value="kk">Қазақша</option><option value="en">English</option></Select>
          </Field>
          {Object.keys(errors).length > 0 && <div className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-800">{Object.values(errors).join('. ')}</div>}
          <div className="mt-6 flex gap-3"><Button className="h-12 flex-1" onClick={() => setStep(1)}>Назад</Button><Button variant="primary" className="h-12 flex-[2]" loading={busy} onClick={submit}>Создать центр</Button></div>
        </>
      )}
      {step === 3 && (
        <>
          <h2 className="mt-2 text-[26px] font-bold">Центр создан 🎉</h2>
          <p className="mt-2 text-sm text-muted">Первые шаги: преподаватель → курс → группа → импорт учеников → расписание. Чеклист есть на главной.</p>
          <Button variant="primary" className="mt-8 h-12 w-full" onClick={() => nav('/')}>Перейти в систему</Button>
        </>
      )}
    </AuthShell>
  )
}
