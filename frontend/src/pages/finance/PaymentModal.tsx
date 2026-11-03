import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, errorsByField } from '../../lib/api'
import { isoDay, money, parseMoney, phone } from '../../lib/format'
import { Button, Field, Input, Modal, Select, paymentMethods } from '../../components/ui'

// Ручная фиксация оплаты (наличные, карта, Kaspi-перевод). Idempotency-Key защищает от двойного нажатия.
export function PaymentModal({ studentId, onClose }: { studentId?: string; onClose: () => void }) {
  const qc = useQueryClient()
  const [sid, setSid] = useState(studentId ?? '')
  const [q, setQ] = useState('')
  const [amount, setAmount] = useState('')
  const [method, setMethod] = useState('kaspi')
  const [paidAt, setPaidAt] = useState(isoDay())
  const [receipt, setReceipt] = useState('')
  const [comment, setComment] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const key = useMemo(() => crypto.randomUUID(), [])
  const search = useQuery({ queryKey: ['search-students', q], queryFn: () => api.get<{ id: string; full_name: string; phone: string | null; balance_tiyn: number }[]>(`/students?limit=8&q=${encodeURIComponent(q)}`), enabled: !studentId && q.length >= 2 })
  const student = useQuery({ queryKey: ['student', sid], queryFn: () => api.get(`/students/${sid}`), enabled: !!sid })
  const balance = student.data?.student.balance_tiyn as number | undefined
  const m = useMutation({
    mutationFn: () => {
      const tiyn = parseMoney(amount)
      if (tiyn == null || tiyn <= 0) throw Object.assign(new Error('Укажите сумму'), {})
      return api.post('/payments', { student_id: sid, amount_tiyn: tiyn, method, paid_at: paidAt, receipt_number: receipt, comment }, { 'Idempotency-Key': key })
    },
    onSuccess: () => { qc.invalidateQueries(); onClose() },
    onError: (e) => setErrors({ ...errorsByField(e), _: (e as Error).message }),
  })
  return (
    <Modal open title="Принять оплату" onClose={onClose} footer={<><Button onClick={onClose}>Отмена</Button><Button variant="primary" disabled={!sid} loading={m.isPending} onClick={() => m.mutate()}>Провести {amount && parseMoney(amount) ? money(parseMoney(amount)) : ''}</Button></>}>
      <div className="space-y-4">
        {!studentId && !sid && (
          <Field label="Ученик">
            <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="ФИО или телефон" />
            <div className="mt-1 divide-y divide-line rounded-lg border border-line empty:hidden">
              {search.data?.map((s) => (
                <button key={s.id} onClick={() => setSid(s.id)} className="flex w-full justify-between px-3 py-2 text-left text-sm hover:bg-gray-50">
                  <span>{s.full_name} <span className="text-muted">{phone(s.phone)}</span></span><span className={s.balance_tiyn < 0 ? 'text-red-700' : ''}>{money(s.balance_tiyn)}</span>
                </button>
              ))}
            </div>
          </Field>
        )}
        {sid && student.data && (
          <div className="flex items-center justify-between rounded-lg bg-gray-50 px-4 py-3 text-sm">
            <span className="font-medium">{student.data.student.full_name}</span>
            <span>Баланс: <b className={balance! < 0 ? 'text-red-700' : ''}>{money(balance)}</b></span>
          </div>
        )}
        <div className="grid grid-cols-2 gap-4">
          <Field label="Сумма, ₸" error={errors.amount_tiyn}>
            <Input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="54 000" />
            {balance != null && balance < 0 && <button type="button" className="mt-1 text-xs text-brand-600" onClick={() => setAmount(String(-balance / 100))}>Погасить долг {money(-balance)}</button>}
          </Field>
          <Field label="Способ"><Select value={method} onChange={(e) => setMethod(e.target.value)}>{Object.entries(paymentMethods).filter(([k]) => k !== 'online').map(([k, l]) => <option key={k} value={k}>{l}</option>)}</Select></Field>
          <Field label="Дата" error={errors.paid_at}><Input type="date" value={paidAt} onChange={(e) => setPaidAt(e.target.value)} /></Field>
          <Field label={method === 'kaspi' ? 'Номер операции Kaspi' : 'Номер чека'}><Input value={receipt} onChange={(e) => setReceipt(e.target.value)} /></Field>
        </div>
        <Field label="Комментарий"><Input value={comment} onChange={(e) => setComment(e.target.value)} /></Field>
        {errors._ && !Object.keys(errors).some((k) => k !== '_') && <div className="text-sm text-red-700">{errors._}</div>}
      </div>
    </Modal>
  )
}
