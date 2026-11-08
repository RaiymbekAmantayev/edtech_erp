import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, onAuthLost, refresh, request, setToken } from './api'

export type Branch = { id: string; name: string; address: string | null; timezone: string }
export type Me = {
  user: { id: string; full_name: string; email: string; locale?: string }
  tenant?: { id: string; name: string; plan: string; status: string; timezone: string; trial_ends_at: string | null; center_type: string }
  permissions?: string[]
  roles?: string[]
  branches?: Branch[]
  employee_id?: string
  all_branches?: boolean
  is_platform_admin?: boolean
}

type Ctx = {
  me: Me | null
  loading: boolean
  can: (perm: string) => boolean
  login: (email: string, password: string) => Promise<void>
  loginWithToken: (token: string) => Promise<void>
  logout: () => Promise<void>
  branchId: string
  setBranchId: (id: string) => void
  isTeacherOnly: boolean
}

const AuthCtx = createContext<Ctx>(null!)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [loading, setLoading] = useState(true)
  const [branchId, setBranch] = useState(() => localStorage.getItem('emp.branch') ?? '')
  const qc = useQueryClient()

  const loadMe = useCallback(async () => {
    const m = await api.get<Me>('/me')
    setMe(m)
    if (m.branches && branchId && !m.branches.some((b) => b.id === branchId)) setBranch('')
  }, [branchId])

  useEffect(() => {
    // кабинет /app входит сам (Telegram или ИИН) — восстановление сессии сотрудника там не нужно
    if (location.pathname.startsWith('/app')) { setLoading(false); return }
    // восстановление сессии по refresh-cookie
    refresh().then(async (ok) => {
      if (ok) await loadMe().catch(() => setMe(null))
      setLoading(false)
    })
    return onAuthLost(() => { setToken(null); setMe(null) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const loginWithToken = async (token: string) => {
    setToken(token)
    qc.clear()
    await loadMe()
  }
  const login = async (email: string, password: string) => {
    const r = await request<{ access_token: string }>('POST', '/auth/login', { email, password })
    await loginWithToken(r.data.access_token)
  }
  const logout = async () => {
    await api.post('/auth/logout').catch(() => {})
    setToken(null)
    setMe(null)
    qc.clear()
  }
  const setBranchId = (id: string) => {
    setBranch(id)
    localStorage.setItem('emp.branch', id)
  }
  const perms = new Set(me?.permissions ?? [])
  const can = (p: string) => perms.has(p)
  const isTeacherOnly = !!me?.roles?.length && me.roles.every((r) => r === 'teacher')

  return (
    <AuthCtx.Provider value={{ me, loading, can, login, loginWithToken, logout, branchId, setBranchId, isTeacherOnly }}>{children}</AuthCtx.Provider>
  )
}

export const useAuth = () => useContext(AuthCtx)
