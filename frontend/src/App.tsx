import { Navigate, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout'
import { DashboardPage } from './pages/DashboardPage'
import { StatementPage } from './pages/StatementPage'
import { ImportPage } from './pages/ImportPage'
import { PeoplePage } from './pages/PeoplePage'
import { ReceiptsPage } from './pages/ReceiptsPage'
import { SettingsPage } from './pages/SettingsPage'

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/statements/:id" element={<StatementPage />} />
        <Route path="/import" element={<ImportPage />} />
        <Route path="/people" element={<PeoplePage />} />
        <Route path="/receipts" element={<ReceiptsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default App
