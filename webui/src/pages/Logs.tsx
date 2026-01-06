import { useState, useEffect, useRef } from 'react';
import type { LogsResponse, ContainersResponse } from '../lib/api';
import { api } from '../lib/api';

interface LogsProps {
  initialContainer?: string;
}

const LINE_OPTIONS = [100, 500, 1000];

export function Logs({ initialContainer }: LogsProps): React.ReactElement {
  const [selectedContainer, setSelectedContainer] = useState<string>(initialContainer || '');
  const [lines, setLines] = useState<number>(100);
  const [logs, setLogs] = useState<LogsResponse | null>(null);
  const [containers, setContainers] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const logRef = useRef<HTMLPreElement>(null);

  // Load container list
  useEffect(() => {
    const loadContainers = async (): Promise<void> => {
      try {
        const data: ContainersResponse = await api.getContainers();
        const names = data.containers.map((c) => c.name).sort();
        setContainers(names);
        if (!selectedContainer && names.length > 0) {
          setSelectedContainer(names[0]);
        }
      } catch (err) {
        console.error('Failed to load containers:', err);
      }
    };
    loadContainers();
  }, [selectedContainer]);

  // Load logs when container or lines change
  useEffect(() => {
    if (!selectedContainer) return;

    const loadLogs = async (): Promise<void> => {
      setIsLoading(true);
      setError(null);
      try {
        const data = await api.getContainerLogs(selectedContainer, lines);
        setLogs(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load logs');
      } finally {
        setIsLoading(false);
      }
    };

    loadLogs();
  }, [selectedContainer, lines]);

  // Scroll to bottom when logs change
  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [logs]);

  const handleRefresh = async (): Promise<void> => {
    if (!selectedContainer) return;
    setIsLoading(true);
    try {
      const data = await api.getContainerLogs(selectedContainer, lines);
      setLogs(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh logs');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="space-y-8 h-full flex flex-col">
      {/* Page Header */}
      <div className="flex items-end justify-between flex-shrink-0">
        <div>
          <h2
            className="font-display text-3xl font-semibold"
            style={{ color: 'var(--color-text-primary)' }}
          >
            Ship's Log
          </h2>
          <p
            className="mt-1 font-mono text-sm"
            style={{ color: 'var(--color-text-muted)' }}
          >
            Container output and event records
          </p>
        </div>
      </div>

      {/* Log Viewer Card */}
      <div className="maritime-card-accent overflow-hidden flex-1 flex flex-col" style={{ minHeight: '500px' }}>
        {/* Controls Header */}
        <div
          className="px-6 py-4 flex flex-wrap items-center gap-6 border-b"
          style={{ borderColor: 'var(--color-border-subtle)' }}
        >
          {/* Container Select */}
          <div className="flex items-center gap-3">
            <label htmlFor="container-select" className="form-label">
              Vessel
            </label>
            <select
              id="container-select"
              value={selectedContainer}
              onChange={(e) => setSelectedContainer(e.target.value)}
              className="form-select"
            >
              {containers.length === 0 && (
                <option value="">Loading...</option>
              )}
              {containers.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>

          {/* Lines Select */}
          <div className="flex items-center gap-3">
            <label htmlFor="lines-select" className="form-label">
              Lines
            </label>
            <select
              id="lines-select"
              value={lines}
              onChange={(e) => setLines(Number(e.target.value))}
              className="form-select"
            >
              {LINE_OPTIONS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </div>

          {/* Refresh Button */}
          <button
            onClick={handleRefresh}
            disabled={isLoading || !selectedContainer}
            className="btn-secondary"
          >
            {isLoading ? (
              <span className="compass-loader-sm" />
            ) : (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            )}
            Refresh
          </button>

          {/* Current container indicator */}
          {selectedContainer && (
            <div className="ml-auto flex items-center gap-2">
              <div className="status-indicator status-info" />
              <span className="font-mono text-sm" style={{ color: 'var(--color-text-secondary)' }}>
                {selectedContainer}
              </span>
            </div>
          )}
        </div>

        {/* Log Content */}
        <div className="flex-1 overflow-hidden">
          {error ? (
            <div className="p-6 flex items-center gap-4">
              <div
                className="w-10 h-10 rounded-lg flex items-center justify-center text-xl flex-shrink-0"
                style={{ background: 'var(--color-danger-bg)', color: 'var(--color-danger)' }}
              >
                ⚠
              </div>
              <div>
                <p className="font-mono text-sm font-semibold" style={{ color: 'var(--color-danger)' }}>
                  Error Loading Logs
                </p>
                <p className="font-mono text-sm mt-1" style={{ color: 'var(--color-text-secondary)' }}>
                  {error}
                </p>
              </div>
            </div>
          ) : !selectedContainer ? (
            <div className="h-full flex items-center justify-center">
              <div className="text-center">
                <div className="text-4xl mb-3" style={{ color: 'var(--color-text-muted)' }}>
                  📜
                </div>
                <p className="font-mono text-sm" style={{ color: 'var(--color-text-muted)' }}>
                  Select a vessel to view its log
                </p>
              </div>
            </div>
          ) : (
            <pre
              ref={logRef}
              className="log-viewer h-full"
            >
              {isLoading && !logs ? (
                <div className="flex items-center gap-3">
                  <span className="compass-loader-sm" />
                  <span style={{ color: 'var(--color-text-muted)' }}>Loading ship's log...</span>
                </div>
              ) : logs?.logs ? (
                logs.logs
              ) : (
                <span style={{ color: '#5a6a5a' }}>No log entries available</span>
              )}
            </pre>
          )}
        </div>

        {/* Footer with metadata */}
        {logs && !error && (
          <div
            className="px-6 py-3 flex items-center justify-between border-t"
            style={{
              borderColor: 'var(--color-border-subtle)',
              background: 'var(--color-bg-tertiary)'
            }}
          >
            <span className="font-mono text-xs" style={{ color: 'var(--color-text-muted)' }}>
              Showing last {logs.lines} lines
            </span>
            <span className="font-mono text-xs" style={{ color: 'var(--color-text-muted)' }}>
              Container: {logs.container}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}
