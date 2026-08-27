// Design-sync bundle entry: the webui is an app, not a library, so this
// barrel is what gives the claude.ai/design converter a library-shaped entry.
// Keep it in step with the reusable components under src/components/.
export { DarkModeToggle } from './components/DarkModeToggle';
export { ErrorBoundary } from './components/ErrorBoundary';
export { LoadingSpinner, LoadingState } from './components/LoadingSpinner';
export { OfflineBanner } from './components/OfflineBanner';
