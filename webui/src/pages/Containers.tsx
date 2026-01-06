import { useState } from 'react';
import type { Container, ContainersResponse } from '../lib/api';
import { api } from '../lib/api';
import { usePolling } from '../hooks/usePolling';
import { LoadingState } from '../components/LoadingSpinner';

interface ContainersProps {
  onViewLogs: (containerId: string) => void;
}

function HealthBadge({ state, health }: { state: string; health?: string }): React.ReactElement {
  if (state !== 'running') {
    return <span className="badge badge-neutral">{state}</span>;
  }

  if (health === 'unhealthy') {
    return <span className="badge badge-danger">unhealthy</span>;
  }

  if (health === 'healthy') {
    return <span className="badge badge-healthy">healthy</span>;
  }

  return <span className="badge badge-info">running</span>;
}

function ContainerRow({
  container,
  onRestart,
  onViewLogs,
  isRestarting,
}: {
  container: Container;
  onRestart: () => void;
  onViewLogs: () => void;
  isRestarting: boolean;
}): React.ReactElement {
  const [showConfirm, setShowConfirm] = useState(false);

  return (
    <tr>
      <td className="px-6 py-4">
        <div className="flex items-center gap-3">
          <div
            className={`status-indicator flex-shrink-0 ${
              container.state !== 'running'
                ? 'status-offline'
                : container.health === 'unhealthy'
                ? 'status-danger'
                : container.health === 'healthy'
                ? 'status-healthy'
                : 'status-info'
            }`}
          />
          <div>
            <div className="cell-primary">{container.name}</div>
            <div className="cell-secondary mt-0.5">{container.id.substring(0, 12)}</div>
          </div>
        </div>
      </td>
      <td className="px-6 py-4">
        <span className="font-mono text-sm" style={{ color: 'var(--color-text-secondary)' }}>
          {container.image}
        </span>
      </td>
      <td className="px-6 py-4">
        <HealthBadge state={container.state} health={container.health} />
      </td>
      <td className="px-6 py-4">
        <span className="font-mono text-sm" style={{ color: 'var(--color-text-muted)' }}>
          {container.status}
        </span>
      </td>
      <td className="px-6 py-4">
        <div className="flex items-center gap-2">
          <button onClick={onViewLogs} className="btn-secondary" style={{ padding: '0.375rem 0.75rem' }}>
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            Logs
          </button>
          {showConfirm ? (
            <>
              <button
                onClick={() => {
                  onRestart();
                  setShowConfirm(false);
                }}
                disabled={isRestarting}
                className="btn-danger"
              >
                {isRestarting ? (
                  <span className="compass-loader-sm" />
                ) : (
                  'Confirm'
                )}
              </button>
              <button
                onClick={() => setShowConfirm(false)}
                className="btn-secondary"
                style={{ padding: '0.375rem 0.75rem' }}
              >
                Cancel
              </button>
            </>
          ) : (
            <button
              onClick={() => setShowConfirm(true)}
              disabled={container.state !== 'running'}
              className="btn-secondary"
              style={{
                padding: '0.375rem 0.75rem',
                opacity: container.state !== 'running' ? 0.5 : 1,
                cursor: container.state !== 'running' ? 'not-allowed' : 'pointer'
              }}
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Restart
            </button>
          )}
        </div>
      </td>
    </tr>
  );
}

export function Containers({ onViewLogs }: ContainersProps): React.ReactElement {
  const [restartingId, setRestartingId] = useState<string | null>(null);

  const {
    data: containersData,
    isLoading,
    refetch,
  } = usePolling<ContainersResponse>({
    fetcher: api.getContainers,
    interval: 5000,
  });

  const handleRestart = async (containerId: string): Promise<void> => {
    setRestartingId(containerId);
    try {
      await api.restartContainer(containerId);
      await refetch();
    } catch (error) {
      console.error('Failed to restart container:', error);
    } finally {
      setRestartingId(null);
    }
  };

  if (isLoading && !containersData) {
    return <LoadingState message="Loading fleet manifest..." />;
  }

  const containers = containersData?.containers || [];

  return (
    <div className="space-y-8">
      {/* Page Header */}
      <div className="flex items-end justify-between">
        <div>
          <h2
            className="font-display text-3xl font-semibold"
            style={{ color: 'var(--color-text-primary)' }}
          >
            Fleet Manifest
          </h2>
          <p
            className="mt-1 font-mono text-sm"
            style={{ color: 'var(--color-text-muted)' }}
          >
            {containers.length} vessels in the fleet
          </p>
        </div>

        {/* Summary badges */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 px-3 py-1.5 rounded" style={{ background: 'var(--color-healthy-bg)' }}>
            <div className="status-indicator status-healthy" style={{ width: '8px', height: '8px' }} />
            <span className="font-mono text-sm" style={{ color: 'var(--color-healthy)' }}>
              {containersData?.summary.running ?? 0} running
            </span>
          </div>
          {(containersData?.summary.unhealthy ?? 0) > 0 && (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded" style={{ background: 'var(--color-danger-bg)' }}>
              <div className="status-indicator status-danger" style={{ width: '8px', height: '8px' }} />
              <span className="font-mono text-sm" style={{ color: 'var(--color-danger)' }}>
                {containersData?.summary.unhealthy} unhealthy
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Table */}
      <div className="maritime-card-accent overflow-hidden">
        <div className="overflow-x-auto">
          <table className="data-table">
            <thead>
              <tr>
                <th className="w-[280px]">Vessel</th>
                <th>Image</th>
                <th className="w-[120px]">Health</th>
                <th className="w-[180px]">Status</th>
                <th className="w-[200px]">Actions</th>
              </tr>
            </thead>
            <tbody>
              {containers.map((container) => (
                <ContainerRow
                  key={container.id}
                  container={container}
                  onRestart={() => handleRestart(container.name)}
                  onViewLogs={() => onViewLogs(container.name)}
                  isRestarting={restartingId === container.name}
                />
              ))}
            </tbody>
          </table>
        </div>

        {containers.length === 0 && (
          <div className="px-6 py-12 text-center">
            <div
              className="text-4xl mb-3"
              style={{ color: 'var(--color-text-muted)' }}
            >
              ⚓
            </div>
            <p
              className="font-mono text-sm"
              style={{ color: 'var(--color-text-muted)' }}
            >
              No vessels found in the fleet
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
