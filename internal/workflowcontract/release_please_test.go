package workflowcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleasePleaseWorkflowContract(t *testing.T) {
	workflowData := loadReleasePleaseWorkflow(t)
	require.NoError(t, validateReleasePlease(workflowData))
}

func TestReleasePleaseWorkflowContractRejectsUnsafeMutations(t *testing.T) {
	workflowData := string(loadReleasePleaseWorkflow(t))
	tests := []struct {
		name        string
		old         string
		replacement string
		expected    string
	}{
		{
			name:        "commented continue-on-error with active false value",
			old:         "        continue-on-error: true",
			replacement: "        # continue-on-error: true\n        continue-on-error: false",
			expected:    "must continue on credential errors",
		},
		{
			name:        "permissive auto-merge condition",
			old:         "        if: ${{ steps.release.outputs.pr && steps.app-token.outcome == 'success' }}",
			replacement: "        if: ${{ steps.release.outputs.pr && steps.app-token.outcome == 'success' || true }}",
			expected:    "must require an exact successful App-token outcome",
		},
		{
			name: "second default-token merge path",
			old:  "      - name: Auto-merge release PR",
			replacement: "      - name: Unsafe fallback merge\n" +
				"        env:\n" +
				"          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n" +
				"        run: gh pr merge 1 --merge\n\n" +
				"      - name: Auto-merge release PR",
			expected: "exactly one active gh pr merge path",
		},
		{
			name: "cross-job default-token merge path",
			old:  "  goreleaser:\n",
			replacement: "  unsafe-merge:\n" +
				"    runs-on: ubuntu-latest\n" +
				"    steps:\n" +
				"      - name: Merge elsewhere\n" +
				"        env:\n" +
				"          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n" +
				"        run: gh pr merge 1 --merge\n\n" +
				"  goreleaser:\n",
			expected: "exactly one active gh pr merge path",
		},
		{
			name:        "merge command overrides app token",
			old:         `          gh pr merge "$PR_NUMBER" --auto --merge`,
			replacement: `          GH_TOKEN="$GITHUB_TOKEN" gh pr merge "$PR_NUMBER" --auto --merge`,
			expected:    "auto-merge must not reference GITHUB_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, workflowData, tt.old, "mutation fixture no longer matches the workflow")
			mutated := strings.Replace(workflowData, tt.old, tt.replacement, 1)
			err := validateReleasePlease([]byte(mutated))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.expected)
		})
	}
}

func TestReleasePleaseWorkflowContractAllowsDefaultTokenForGeneration(t *testing.T) {
	workflowData := string(loadReleasePleaseWorkflow(t))
	old := "          manifest-file: .release-please-manifest.json"
	replacement := old + "\n          token: ${{ github.token }}"
	require.Contains(t, workflowData, old, "acceptance fixture no longer matches the workflow")

	mutated := strings.Replace(workflowData, old, replacement, 1)
	require.NoError(t, validateReleasePlease([]byte(mutated)))
}

func TestReleasePleaseWorkflowContractIgnoresNonCommandMentions(t *testing.T) {
	workflowData := string(loadReleasePleaseWorkflow(t))
	tests := []struct {
		name        string
		old         string
		replacement string
	}{
		{
			name: "echo mentions merge command",
			old:  "      - name: Auto-merge release PR",
			replacement: "      - name: Explain release safety\n" +
				`        run: echo "never run gh pr merge"` + "\n\n" +
				"      - name: Auto-merge release PR",
		},
		{
			name:        "comment mentions default token",
			old:         "          PR_NUMBER=$(echo \"$RELEASE_PR\" | jq -r '.number')",
			replacement: "          # Never replace the App token with $GITHUB_TOKEN\n          PR_NUMBER=$(echo \"$RELEASE_PR\" | jq -r '.number')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, workflowData, tt.old, "acceptance fixture no longer matches the workflow")
			mutated := strings.Replace(workflowData, tt.old, tt.replacement, 1)
			require.NoError(t, validateReleasePlease([]byte(mutated)))
		})
	}
}

func loadReleasePleaseWorkflow(t *testing.T) []byte {
	t.Helper()
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release-please.yml")
	workflowData, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	return workflowData
}
