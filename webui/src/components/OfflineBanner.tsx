interface OfflineBannerProps {
  lastUpdated: Date | null;
  onRetry?: () => void;
}

export function OfflineBanner({ lastUpdated, onRetry }: OfflineBannerProps): React.ReactElement {
  const formatTime = (date: Date): string => {
    return date.toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  };

  return (
    <div className="alert-banner relative">
      {/* Hazard stripe accent */}
      <div
        className="absolute left-0 top-0 bottom-0 w-1"
        style={{
          background: 'repeating-linear-gradient(45deg, #fbbf24, #fbbf24 4px, #dc2626 4px, #dc2626 8px)'
        }}
      />

      <div className="max-w-7xl mx-auto px-6 flex items-center justify-between w-full">
        <div className="flex items-center gap-4">
          {/* Warning icon */}
          <div className="flex items-center justify-center w-8 h-8 rounded bg-red-800/50">
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
              />
            </svg>
          </div>

          <div>
            <span className="font-mono text-sm font-semibold uppercase tracking-wide">
              Daemon Offline
            </span>
            {lastUpdated && (
              <span className="ml-3 font-mono text-xs opacity-75">
                Last contact: {formatTime(lastUpdated)}
              </span>
            )}
          </div>
        </div>

        {onRetry && (
          <button
            onClick={onRetry}
            className="px-4 py-1.5 font-mono text-xs font-semibold uppercase tracking-wide bg-white/10 hover:bg-white/20 rounded transition-colors"
          >
            Retry Connection
          </button>
        )}
      </div>
    </div>
  );
}
