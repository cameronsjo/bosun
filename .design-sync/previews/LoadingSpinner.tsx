import { LoadingSpinner } from 'webui';

// size="sm" is omitted deliberately: .compass-loader-sm alone carries no
// ::before content in the webui CSS, so the sm spinner renders invisible —
// an upstream webui bug recorded in .design-sync/NOTES.md.
export const Medium = () => <LoadingSpinner />;

export const InCard = () => (
  <div className="maritime-card" style={{ display: 'flex', alignItems: 'center', gap: '1rem', padding: '1rem 1.25rem', width: 300 }}>
    <LoadingSpinner />
    <span className="font-mono" style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.04em', color: 'var(--color-text-muted)' }}>
      Fetching crew status
    </span>
  </div>
);
