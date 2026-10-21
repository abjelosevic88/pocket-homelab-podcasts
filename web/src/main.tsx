import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/podcasts.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { PlayerProvider } from './lib/player'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Home from './pages/Home'
import Podcasts from './pages/Podcasts'
import Podcast from './pages/Podcast'
import Episodes from './pages/Episodes'
import EpisodePage from './pages/Episode'
import Queue from './pages/Queue'
import Playlists from './pages/Playlists'
import PlaylistPage from './pages/Playlist'
import Saved from './pages/Saved'
import History from './pages/History'
import Downloads from './pages/Downloads'
import Add from './pages/Add'
import Stats from './pages/Stats'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

/** Signed in: one player for the whole app, so the audio keeps playing from the
 *  pages into Settings and back. */
function Signed() {
  return <Gate><PlayerProvider><Outlet /></PlayerProvider></Gate>
}

function Landing() {
  const { settings } = useApp()
  const to = settings?.landing === 'podcasts' ? '/podcasts' : settings?.landing === 'queue' ? '/queue' : settings?.landing === 'episodes' ? '/episodes' : '/home'
  return <Navigate to={to} replace />
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route element={<Signed />}>
        <Route path="/settings/:tab?" element={<ErrorBoundary where="settings"><Settings /></ErrorBoundary>} />
        <Route element={<ErrorBoundary where="podcasts"><Layout /></ErrorBoundary>}>
          <Route path="/" element={<Landing />} />
          <Route path="/home" element={<Home />} />
          <Route path="/podcasts" element={<Podcasts />} />
          <Route path="/podcasts/:id" element={<Podcast />} />
          <Route path="/episodes" element={<Episodes />} />
          <Route path="/episodes/:id" element={<EpisodePage />} />
          <Route path="/queue" element={<Queue />} />
          <Route path="/playlists" element={<Playlists />} />
          <Route path="/playlists/:id" element={<PlaylistPage />} />
          <Route path="/saved" element={<Saved />} />
          <Route path="/history" element={<History />} />
          <Route path="/downloads" element={<Downloads />} />
          <Route path="/add" element={<Add />} />
          <Route path="/stats" element={<Stats />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
