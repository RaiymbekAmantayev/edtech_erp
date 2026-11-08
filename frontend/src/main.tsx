import { StrictMode, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './index.css'
import { AuthProvider, useAuth } from './lib/auth'
import { ApiError } from './lib/api'
import { Spinner } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import Register from './pages/Register'
import Dashboard from './pages/Dashboard'
import Students from './pages/Students'
import StudentCard from './pages/StudentCard'
import Parents from './pages/Parents'
import Teachers from './pages/Teachers'
import Groups from './pages/Groups'
import GroupCard from './pages/GroupCard'
import Schedule from './pages/Schedule'
import Attendance from './pages/Attendance'
import AttendanceMark from './pages/AttendanceMark'
import MiniApp from './miniapp/MiniApp'
import Finance from './pages/finance/Finance'
import Leads from './pages/leads/Leads'
import Reports from './pages/Reports'
import Settings from './pages/settings/Settings'
import PlatformLayout from './pages/platform/PlatformLayout'
import Organizations from './pages/platform/Organizations'
import OrganizationCard from './pages/platform/OrganizationCard'
import { PlatformPlans, PlatformSystem, PlatformHolidays } from './pages/platform/PlatformMisc'

const qc = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, refetchOnWindowFocus: false, retry: (n, e) => !(e instanceof ApiError && e.status < 500) && n < 2 },
  },
})

function Guard({ children, perm }: { children: ReactNode; perm?: string | string[] }) {
  const { me, loading, can } = useAuth()
  if (loading) return <Spinner className="h-full" />
  if (!me) return <Navigate to="/login" replace />
  if (me.is_platform_admin) return <Navigate to="/platform" replace />
  if (perm && !(Array.isArray(perm) ? perm.some(can) : can(perm))) return <div className="p-10 text-center text-muted">Недостаточно прав для этого раздела</div>
  return <>{children}</>
}

function PlatformGuard({ children }: { children: ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) return <Spinner className="h-full" />
  if (!me) return <Navigate to="/login" replace />
  if (!me.is_platform_admin) return <Navigate to="/" replace />
  return <>{children}</>
}

function Public({ children }: { children: ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) return <Spinner className="h-full" />
  if (me) return <Navigate to={me.is_platform_admin ? '/platform' : '/'} replace />
  return <>{children}</>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<Public><Login /></Public>} />
            <Route path="/register" element={<Public><Register /></Public>} />
            {/* личный кабинет учеников, родителей и преподавателей (Telegram Mini App) */}
            <Route path="/app/*" element={<MiniApp />} />
            {/* мобильная отметка посещаемости (кабинет преподавателя / Mini App) — без сайдбара */}
            <Route path="/attendance/:lessonId" element={<Guard perm={['attendance.mark', 'attendance.view']}><AttendanceMark /></Guard>} />
            <Route element={<Guard><Layout /></Guard>}>
              <Route index element={<Dashboard />} />
              <Route path="students" element={<Guard perm="students.view"><Students /></Guard>} />
              <Route path="students/:id" element={<Guard perm="students.view"><StudentCard /></Guard>} />
              <Route path="parents" element={<Guard perm="parents.view"><Parents /></Guard>} />
              <Route path="teachers" element={<Guard perm="employees.view"><Teachers /></Guard>} />
              <Route path="groups" element={<Guard perm="groups.view"><Groups /></Guard>} />
              <Route path="groups/:id" element={<Guard perm="groups.view"><GroupCard /></Guard>} />
              <Route path="schedule" element={<Guard perm={['schedule.view', 'attendance.mark']}><Schedule /></Guard>} />
              <Route path="attendance" element={<Guard perm={['attendance.view', 'attendance.mark']}><Attendance /></Guard>} />
              <Route path="finance" element={<Guard perm="finance.view"><Finance /></Guard>} />
              <Route path="leads" element={<Guard perm="leads.view"><Leads /></Guard>} />
              <Route path="reports" element={<Guard perm="reports.view"><Reports /></Guard>} />
              <Route path="settings/*" element={<Guard perm="settings.manage"><Settings /></Guard>} />
            </Route>
            <Route path="/platform" element={<PlatformGuard><PlatformLayout /></PlatformGuard>}>
              <Route index element={<Organizations />} />
              <Route path="organizations/:id" element={<OrganizationCard />} />
              <Route path="plans" element={<PlatformPlans />} />
              <Route path="system" element={<PlatformSystem />} />
              <Route path="holidays" element={<PlatformHolidays />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
