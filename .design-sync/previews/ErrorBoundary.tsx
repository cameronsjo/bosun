import { ErrorBoundary } from 'webui';

function MastSnapped(): never {
  throw new Error('The mast snapped: container runtime unreachable');
}

export const PassesChildrenThrough = () => (
  <ErrorBoundary>
    <div className="maritime-card-accent" style={{ padding: '1rem 1.25rem', width: 320 }}>
      <p style={{ margin: 0, fontWeight: 600 }}>All hands accounted for</p>
      <p className="font-mono" style={{ margin: '0.25rem 0 0', fontSize: 12, color: 'var(--color-text-muted)' }}>
        children render untouched
      </p>
    </div>
  </ErrorBoundary>
);

export const CustomFallback = () => (
  <ErrorBoundary
    fallback={
      <div className="maritime-card" style={{ padding: '1rem 1.25rem', width: 360, borderColor: 'var(--color-danger)' }}>
        <p className="font-mono" style={{ margin: 0, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.04em', color: 'var(--color-danger)' }}>
          Mayday
        </p>
        <p style={{ margin: '0.375rem 0 0' }}>The dashboard ran aground. Reload to refloat.</p>
      </div>
    }
  >
    <MastSnapped />
  </ErrorBoundary>
);
