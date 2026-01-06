interface LoadingSpinnerProps {
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}

export function LoadingSpinner({ size = 'md', className = '' }: LoadingSpinnerProps): React.ReactElement {
  const sizeClass = size === 'sm' ? 'compass-loader-sm' : 'compass-loader';

  return (
    <div
      className={`${sizeClass} ${className}`}
      role="status"
      aria-label="Loading"
    />
  );
}

interface LoadingStateProps {
  message?: string;
}

export function LoadingState({ message = 'Loading...' }: LoadingStateProps): React.ReactElement {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <div className="compass-loader" />
      <p
        className="mt-6 font-mono text-sm uppercase tracking-wide"
        style={{ color: 'var(--color-text-muted)' }}
      >
        {message}
      </p>
    </div>
  );
}
