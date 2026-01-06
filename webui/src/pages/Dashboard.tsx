import { useState } from 'react';
import type { APIStatus, ContainersResponse } from '../lib/api';
import { api } from '../lib/api';
import { usePolling } from '../hooks/usePolling';
import { LoadingState } from '../components/LoadingSpinner';

interface DashboardProps {
  onNavigate: (page: 'containers' | 'logs') => void;
}

export function Dashboard({ onNavigate }: DashboardProps): React.ReactElement {
  const [isTriggering, setIsTriggering] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);

  const {
    data: status,
    isLoading: statusLoading,
    refetch: refetchStatus,
  } = usePolling<APIStatus>({
    fetcher: api.getStatus,
    interval: 5000,
  });

  const { data: containers } = usePolling<ContainersResponse>({
    fetcher: api.getContainers,
    interval: 5000,
  });

  const handleTrigger = async (): Promise<void> => {
    setShowConfirm(false);
    setIsTriggering(true);
    try {
      await api.trigger('webui');
      await refetchStatus();
    } catch (error) {
      console.error('Failed to trigger reconcile:', error);
    } finally {
      setIsTriggering(false);
    }
  };

  if (statusLoading && !status) {
    return <LoadingState message="Establishing connection to daemon..." />;
  }

  const isReconciling = status?.state === 'reconciling';
  const triggerDisabled = isReconciling || isTriggering;

  // Format next poll time
  const getNextPollIn = (): string | null => {
    if (!status?.next_poll) return null;
    const next = new Date(status.next_poll);
    const now = new Date();
    const diffMs = next.getTime() - now.getTime();
    if (diffMs <= 0) return 'Now';
    const diffMins = Math.round(diffMs / 60000);
    if (diffMins < 60) return `${diffMins}m`;
    const hours = Math.floor(diffMins / 60);
    const mins = diffMins % 60;
    return `${hours}h ${mins}m`;
  };

  return (
    <div className="space-y-8">
      {/* Page Header */}
      <div className="flex items-end justify-between">
        <div>
          <h2
            className="font-display text-3xl font-semibold"
            style={{ color: 'var(--color-text-primary)' }}
          >
            Bridge Overview
          </h2>
          <p
            className="mt-1 font-mono text-sm"
            style={{ color: 'var(--color-text-muted)' }}
          >
            Daemon status and fleet operations
          </p>
        </div>

        {/* Manual Trigger */}
        <div className="flex items-center gap-3">
          {showConfirm ? (
            <>
              <span
                className="font-mono text-sm"
                style={{ color: 'var(--color-text-secondary)' }}
              >
                Deploy now?
              </span>
              <button onClick={handleTrigger} className="btn-primary">
                Confirm
              </button>
              <button onClick={() => setShowConfirm(false)} className="btn-secondary">
                Cancel
              </button>
            </>
          ) : (
            <button
              onClick={() => setShowConfirm(true)}
              disabled={triggerDisabled}
              className="btn-primary"
            >
              {isReconciling ? (
                <>
                  <span className="compass-loader-sm" />
                  Reconciling...
                </>
              ) : isTriggering ? (
                <>
                  <span className="compass-loader-sm" />
                  Deploying...
                </>
              ) : (
                <>
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                  </svg>
                  Trigger Reconcile
                </>
              )}
            </button>
          )}
        </div>
      </div>

      {/* Status Cards Grid */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Daemon Health Card */}
        <div className="maritime-card-accent p-6 animate-fade-in-up opacity-0 animation-delay-1">
          <div className="flex items-start justify-between">
            <div>
              <p className="form-label">Daemon Health</p>
              <div className="mt-3 flex items-center gap-3">
                <div
                  className={`status-indicator ${
                    status?.health === 'healthy' ? 'status-healthy' : 'status-warning'
                  }`}
                />
                <span className="stat-value capitalize">
                  {status?.health || 'Unknown'}
                </span>
              </div>
              <p className="stat-sublabel mt-2">
                Uptime: {status?.uptime || '—'}
              </p>
            </div>
            <div
              className="w-12 h-12 rounded-lg flex items-center justify-center text-2xl"
              style={{
                background: 'var(--color-healthy-bg)',
                color: 'var(--color-healthy)'
              }}
            >
              ⚙
            </div>
          </div>
        </div>

        {/* State Card */}
        <div className="maritime-card-accent p-6 animate-fade-in-up opacity-0 animation-delay-2">
          <div className="flex items-start justify-between">
            <div>
              <p className="form-label">Current State</p>
              <div className="mt-3 flex items-center gap-3">
                {isReconciling && (
                  <div className="status-indicator status-info status-active" />
                )}
                <span className="stat-value capitalize">
                  {status?.state || 'Unknown'}
                </span>
              </div>
              {status?.last_reconcile && (
                <p className="stat-sublabel mt-2">
                  Last: {new Date(status.last_reconcile).toLocaleString()}
                </p>
              )}
            </div>
            <div
              className="w-12 h-12 rounded-lg flex items-center justify-center text-2xl"
              style={{
                background: 'var(--color-info-bg)',
                color: 'var(--color-info)'
              }}
            >
              ⎈
            </div>
          </div>
        </div>

        {/* Next Poll Card */}
        <div className="maritime-card-accent p-6 animate-fade-in-up opacity-0 animation-delay-3">
          <div className="flex items-start justify-between">
            <div>
              <p className="form-label">Next Poll</p>
              <p className="stat-value mt-3">
                {getNextPollIn() || 'Disabled'}
              </p>
              {status?.poll_interval && (
                <p className="stat-sublabel mt-2">
                  Interval: {Math.round(status.poll_interval / 60)}m
                </p>
              )}
            </div>
            <div
              className="w-12 h-12 rounded-lg flex items-center justify-center text-2xl"
              style={{
                background: 'var(--color-warning-bg)',
                color: 'var(--color-warning)'
              }}
            >
              ⏱
            </div>
          </div>
        </div>
      </div>

      {/* Last Error */}
      {status?.last_error && (
        <div
          className="maritime-card p-5 animate-fade-in-up opacity-0 animation-delay-4"
          style={{
            borderColor: 'var(--color-danger)',
            background: 'var(--color-danger-bg)'
          }}
        >
          <div className="flex items-start gap-4">
            <div
              className="w-10 h-10 rounded-lg flex items-center justify-center text-xl flex-shrink-0"
              style={{
                background: 'var(--color-danger)',
                color: 'white'
              }}
            >
              ⚠
            </div>
            <div>
              <h4
                className="font-mono text-sm font-semibold uppercase tracking-wide"
                style={{ color: 'var(--color-danger)' }}
              >
                Last Error
              </h4>
              <p
                className="mt-2 font-mono text-sm"
                style={{ color: 'var(--color-text-primary)' }}
              >
                {status.last_error}
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Fleet Summary */}
      <div className="maritime-card-accent overflow-hidden animate-fade-in-up opacity-0 animation-delay-4">
        <div
          className="px-6 py-4 flex items-center justify-between border-b"
          style={{ borderColor: 'var(--color-border-subtle)' }}
        >
          <div>
            <h3
              className="font-display text-lg font-semibold"
              style={{ color: 'var(--color-text-primary)' }}
            >
              Container Fleet
            </h3>
            <p
              className="font-mono text-xs mt-0.5"
              style={{ color: 'var(--color-text-muted)' }}
            >
              Overview of all managed vessels
            </p>
          </div>
          <button
            onClick={() => onNavigate('containers')}
            className="btn-secondary"
          >
            View All Containers →
          </button>
        </div>

        <div className="p-6">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
            {/* Total */}
            <div className="text-center">
              <p className="stat-value-lg" style={{ color: 'var(--color-text-primary)' }}>
                {containers?.summary.total ?? '—'}
              </p>
              <p className="stat-label">Total</p>
            </div>

            {/* Running */}
            <div className="text-center">
              <p className="stat-value-lg" style={{ color: 'var(--color-healthy)' }}>
                {containers?.summary.running ?? '—'}
              </p>
              <p className="stat-label">Running</p>
            </div>

            {/* Stopped */}
            <div className="text-center">
              <p className="stat-value-lg" style={{ color: 'var(--color-text-muted)' }}>
                {containers?.summary.stopped ?? '—'}
              </p>
              <p className="stat-label">Stopped</p>
            </div>

            {/* Unhealthy */}
            <div className="text-center">
              <p className="stat-value-lg" style={{ color: 'var(--color-danger)' }}>
                {containers?.summary.unhealthy ?? '—'}
              </p>
              <p className="stat-label">Unhealthy</p>
            </div>
          </div>

          {/* Visual Fleet Representation */}
          {containers && containers.summary.total > 0 && (
            <div className="mt-6 pt-6 border-t" style={{ borderColor: 'var(--color-border-subtle)' }}>
              <p className="form-label mb-3">Fleet Status</p>
              <div className="flex gap-1 h-3 rounded overflow-hidden">
                {containers.summary.running > 0 && (
                  <div
                    className="transition-all duration-500"
                    style={{
                      width: `${(containers.summary.running / containers.summary.total) * 100}%`,
                      background: 'var(--color-healthy)'
                    }}
                  />
                )}
                {containers.summary.stopped > 0 && (
                  <div
                    className="transition-all duration-500"
                    style={{
                      width: `${(containers.summary.stopped / containers.summary.total) * 100}%`,
                      background: 'var(--color-text-muted)'
                    }}
                  />
                )}
                {containers.summary.unhealthy > 0 && (
                  <div
                    className="transition-all duration-500"
                    style={{
                      width: `${(containers.summary.unhealthy / containers.summary.total) * 100}%`,
                      background: 'var(--color-danger)'
                    }}
                  />
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
