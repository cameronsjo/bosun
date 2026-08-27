#!/usr/bin/env bash
set -uo pipefail
css=ds-bundle/_ds_bundle.css
missing=0
classes="maritime-bg maritime-card maritime-card-accent btn-primary btn-secondary btn-danger status-indicator status-healthy status-warning status-danger status-info status-offline status-active badge badge-healthy badge-warning badge-danger badge-info badge-neutral nav-item nav-item-active data-table cell-primary cell-secondary log-viewer stat-value stat-value-lg stat-label stat-sublabel form-select form-label alert-banner rope-divider compass-loader font-display font-mono animate-fade-in animate-fade-in-up"
tokens="--color-bg-primary --color-bg-secondary --color-bg-tertiary --color-text-primary --color-text-secondary --color-text-muted --color-border --color-border-subtle --color-brass --color-brass-light --color-brass-dark --color-navy --color-ocean --color-healthy --color-warning --color-danger --color-info --font-display --font-body --font-mono"
for c in $classes; do
  grep -q "\.${c}[ ,:{.]" "$css" || { echo "MISSING class: $c"; missing=1; }
done
for t in $tokens; do
  grep -qF -- "${t}:" "$css" || { echo "MISSING token: $t"; missing=1; }
done
for comp in DarkModeToggle ErrorBoundary LoadingSpinner LoadingState OfflineBanner; do
  [ -d "ds-bundle/components/general/$comp" ] || { echo "MISSING component dir: $comp"; missing=1; }
done
[ "$missing" -eq 0 ] && echo "CONVENTIONS VALID: all names verify" || echo "CONVENTIONS INVALID"
exit $missing
