import { LoadingState } from 'webui';

export const Default = () => (
  <div style={{ width: 420 }}>
    <LoadingState />
  </div>
);

export const CustomMessage = () => (
  <div style={{ width: 420 }}>
    <LoadingState message="Hoisting the mainsail..." />
  </div>
);
