import { DarkModeToggle } from 'webui';

export const LightModeActive = () => (
  <DarkModeToggle isDark={false} onToggle={() => {}} />
);

export const DarkModeActive = () => (
  <DarkModeToggle isDark={true} onToggle={() => {}} />
);

export const InToolbar = () => (
  <div className="maritime-card" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.75rem 1rem', width: 320 }}>
    <span className="font-mono" style={{ fontSize: 13, letterSpacing: '0.03em', textTransform: 'uppercase', color: 'var(--color-text-secondary)' }}>
      Bridge Controls
    </span>
    <DarkModeToggle isDark={false} onToggle={() => {}} />
  </div>
);
