#!/bin/sh

set -eu

workflow=".github/workflows/release-please.yml"

extract_step() {
	step_name=$1
	awk -v step_name="$step_name" '
		/^      - name: / {
			if (found) {
				exit
			}
			found = ($0 == "      - name: " step_name)
		}
		found { print }
	' "$workflow"
}

require_text() {
	block=$1
	expected=$2
	description=$3

	if ! printf '%s\n' "$block" | grep -Fq -- "$expected"; then
		echo "release workflow contract failed: $description" >&2
		exit 1
	fi
}

reject_text() {
	block=$1
	rejected=$2
	description=$3

	if printf '%s\n' "$block" | grep -Fq -- "$rejected"; then
		echo "release workflow contract failed: $description" >&2
		exit 1
	fi
}

token_step=$(extract_step "Generate App Token")
missing_config_step=$(extract_step "Release App not configured")
warning_step=$(extract_step "Release App token unavailable")
merge_step=$(extract_step "Auto-merge release PR")

require_text "$token_step" "continue-on-error: true" \
	"token generation must not erase a successfully generated release PR"
require_text "$token_step" "private-key: \${{ secrets.RELEASE_APP_PRIVATE_KEY }}" \
	"token generation must use the configured private-key secret"
require_text "$missing_config_step" '::warning::' \
	"a missing App ID must emit an explicit warning"
require_text "$warning_step" "steps.app-token.outcome == 'failure'" \
	"credential failures must emit an explicit warning"
require_text "$warning_step" '::warning::' \
	"credential failure diagnostics must be visible in the workflow summary"
require_text "$warning_step" 'RELEASE_APP_PRIVATE_KEY' \
	"credential failure diagnostics must identify the secret that needs rotation"
require_text "$merge_step" "steps.app-token.outcome == 'success'" \
	"auto-merge must require a successfully generated App token"
require_text "$merge_step" "GH_TOKEN: \${{ steps.app-token.outputs.token }}" \
	"auto-merge must use only the App token"
reject_text "$merge_step" 'secrets.GITHUB_TOKEN' \
	"auto-merge must not fall back to GITHUB_TOKEN"

echo "release workflow safety contract passed"
