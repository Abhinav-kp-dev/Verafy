import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout'
import { LiveProvider } from './live'
import { BatchUpload } from './pages/BatchUpload'
import { Dashboard } from './pages/Dashboard'
import { History } from './pages/History'
import { Patients } from './pages/Patients'
import { PreVisit } from './pages/PreVisit'
import { Reports } from './pages/Reports'
import { Review } from './pages/Review'
import { Settings } from './pages/Settings'
import { Verify } from './pages/Verify'

export default function App() {
  return (
    <LiveProvider>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/verify" element={<Verify />} />
            <Route path="/batch" element={<BatchUpload />} />
            <Route path="/patients" element={<Patients />} />
            <Route path="/history" element={<History />} />
            <Route path="/review" element={<Review />} />
            <Route path="/previsit" element={<PreVisit />} />
            <Route path="/reports" element={<Reports />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </LiveProvider>
  )
}
