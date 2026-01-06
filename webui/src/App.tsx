import { useState } from 'react';
import type { APIStatus } from './lib/api';
import { api } from './lib/api';
import { usePolling } from './hooks/usePolling';
import { useDarkMode } from './hooks/useDarkMode';
import { ErrorBoundary } from './components/ErrorBoundary';
import { DarkModeToggle } from './components/DarkModeToggle';
import { OfflineBanner } from './components/OfflineBanner';
import { Dashboard } from './pages/Dashboard';
import { Containers } from './pages/Containers';
import { Logs } from './pages/Logs';

type Page = 'dashboard' | 'containers' | 'logs';

function App(): React.ReactElement {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');
  const [logsContainer, setLogsContainer] = useState<string | undefined>();
  const { isDark, toggleDarkMode } = useDarkMode();

  const {
    isOffline,
    lastUpdated,
    refetch,
  } = usePolling<APIStatus>({
    fetcher: api.getStatus,
    interval: 5000,
  });

  const handleViewLogs = (containerId: string): void => {
    setLogsContainer(containerId);
    setCurrentPage('logs');
  };

  const handleNavigate = (page: 'containers' | 'logs'): void => {
    if (page === 'logs') {
      setLogsContainer(undefined);
    }
    setCurrentPage(page);
  };

  return (
    <ErrorBoundary>
      <div className="maritime-bg min-h-screen">
        {/* Offline Banner */}
        {isOffline && (
          <OfflineBanner lastUpdated={lastUpdated} onRetry={refetch} />
        )}

        {/* Header */}
        <header
          className="relative z-10 border-b"
          style={{
            background: 'var(--color-bg-card)',
            borderColor: 'var(--color-border-subtle)'
          }}
        >
          {/* Brass accent line */}
          <div
            className="absolute top-0 left-0 right-0 h-[3px]"
            style={{
              background: 'linear-gradient(90deg, var(--color-brass-dark), var(--color-brass), var(--color-brass-dark))'
            }}
          />

          <div className="max-w-7xl mx-auto px-6 py-4 flex items-center justify-between">
            <div className="flex items-center gap-8">
              {/* Logo */}
              <div className="flex items-center gap-3">
                <div
                  className="w-10 h-10 rounded-lg flex items-center justify-center text-2xl"
                  style={{
                    background: 'linear-gradient(135deg, var(--color-navy), var(--color-navy-light))',
                    boxShadow: 'var(--shadow-md)'
                  }}
                >
                  ⚓
                </div>
                <div>
                  <h1
                    className="font-display text-xl font-semibold tracking-tight"
                    style={{ color: 'var(--color-text-primary)' }}
                  >
                    Bosun
                  </h1>
                  <p
                    className="text-[10px] font-mono uppercase tracking-widest"
                    style={{ color: 'var(--color-text-muted)' }}
                  >
                    Command Center
                  </p>
                </div>
              </div>

              {/* Navigation */}
              <nav className="flex items-center gap-1">
                <button
                  onClick={() => setCurrentPage('dashboard')}
                  className={`nav-item ${currentPage === 'dashboard' ? 'nav-item-active' : ''}`}
                >
                  Bridge
                </button>
                <button
                  onClick={() => setCurrentPage('containers')}
                  className={`nav-item ${currentPage === 'containers' ? 'nav-item-active' : ''}`}
                >
                  Fleet
                </button>
                <button
                  onClick={() => {
                    setLogsContainer(undefined);
                    setCurrentPage('logs');
                  }}
                  className={`nav-item ${currentPage === 'logs' ? 'nav-item-active' : ''}`}
                >
                  Ship's Log
                </button>
              </nav>
            </div>

            {/* Right side controls */}
            <div className="flex items-center gap-4">
              {/* Connection status indicator */}
              <div className="flex items-center gap-2">
                <div className={`status-indicator ${isOffline ? 'status-offline' : 'status-healthy'}`} />
                <span
                  className="font-mono text-xs uppercase tracking-wide"
                  style={{ color: 'var(--color-text-muted)' }}
                >
                  {isOffline ? 'Offline' : 'Online'}
                </span>
              </div>

              <div
                className="w-px h-6"
                style={{ background: 'var(--color-border)' }}
              />

              <DarkModeToggle isDark={isDark} onToggle={toggleDarkMode} />
            </div>
          </div>
        </header>

        {/* Main Content */}
        <main className="relative z-10 max-w-7xl mx-auto px-6 py-8">
          <div className="animate-fade-in">
            {currentPage === 'dashboard' && (
              <Dashboard onNavigate={handleNavigate} />
            )}
            {currentPage === 'containers' && (
              <Containers onViewLogs={handleViewLogs} />
            )}
            {currentPage === 'logs' && (
              <Logs initialContainer={logsContainer} />
            )}
          </div>
        </main>

        {/* Footer */}
        <footer
          className="relative z-10 py-6 text-center border-t"
          style={{ borderColor: 'var(--color-border-subtle)' }}
        >
          <p
            className="font-mono text-xs uppercase tracking-widest"
            style={{ color: 'var(--color-text-muted)' }}
          >
            Bosun WebUI · Fair Winds & Following Seas
          </p>
        </footer>
      </div>
    </ErrorBoundary>
  );
}

export default App;
