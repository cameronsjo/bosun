import { OfflineBanner } from 'webui';

export const WithLastContact = () => (
  <div style={{ width: 720 }}>
    <OfflineBanner lastUpdated={new Date('2026-08-27T14:32:07')} onRetry={() => {}} />
  </div>
);

export const NoTimestamp = () => (
  <div style={{ width: 720 }}>
    <OfflineBanner lastUpdated={null} />
  </div>
);
