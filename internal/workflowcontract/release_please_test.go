package workflowcontract

import (
	"os"
	"os/exec"
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
		{
			name:        "dispatch tag is optional",
			old:         "        required: true",
			replacement: "        required: false",
			expected:    "tag must be a required string without a default",
		},
		{
			name:        "release generation runs during dispatch",
			old:         "    if: ${{ github.event_name == 'push' }}",
			replacement: "    if: ${{ github.event_name == 'push' || github.event_name == 'workflow_dispatch' }}",
			expected:    "must run only for push events",
		},
		{
			name:        "resolver omits always for skipped dependency",
			old:         "    if: ${{ always() && (github.event_name == 'workflow_dispatch' || needs.release-please.outputs.release_created == 'true') }}",
			replacement: "    if: ${{ github.event_name == 'workflow_dispatch' || needs.release-please.outputs.release_created == 'true' }}",
			expected:    "must safely admit dispatch or a created release",
		},
		{
			name:        "goreleaser accepts a failed resolver",
			old:         "    if: ${{ always() && needs.resolve-release-tag.result == 'success' && needs.resolve-release-tag.outputs.tag_name != '' }}",
			replacement: "    if: ${{ always() && needs.resolve-release-tag.outputs.tag_name != '' }}",
			expected:    "must require a successful non-empty resolved tag",
		},
		{
			name:        "checkout bypasses resolved tag",
			old:         "          ref: ${{ needs.resolve-release-tag.outputs.tag_name }}",
			replacement: "          ref: ${{ needs.release-please.outputs.tag_name }}",
			expected:    "checkout must use the resolved tag",
		},
		{
			name:        "version bypasses resolved tag",
			old:         "          TAG_NAME: ${{ needs.resolve-release-tag.outputs.tag_name }}",
			replacement: "          TAG_NAME: ${{ needs.release-please.outputs.tag_name }}",
			expected:    "version extraction must use the resolved tag",
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

func TestResolveReleaseTagScript(t *testing.T) {
	resolverScript := loadResolverScript(t)
	tests := []struct {
		name             string
		eventName        string
		dispatchTag      string
		releasePleaseTag string
		failCommand      string
		wantTag          string
		wantCalls        []string
		wantError        string
	}{
		{
			name:             "push uses release-please output without API lookup",
			eventName:        "push",
			dispatchTag:      "not-a-tag",
			releasePleaseTag: "v1.2.3",
			wantTag:          "v1.2.3",
		},
		{
			name:        "dispatch validates existing tag and release",
			eventName:   "workflow_dispatch",
			dispatchTag: "v0.40.6",
			wantTag:     "v0.40.6",
			wantCalls: []string{
				"api --silent repos/cameronsjo/bosun/git/ref/tags/v0.40.6",
				"release view v0.40.6 --repo cameronsjo/bosun",
			},
		},
		{
			name:        "dispatch rejects missing prefix before API lookup",
			eventName:   "workflow_dispatch",
			dispatchTag: "0.40.6",
			wantError:   "release tag must be an explicit v-prefixed semantic version",
		},
		{
			name:        "dispatch rejects shell metacharacters before API lookup",
			eventName:   "workflow_dispatch",
			dispatchTag: "v0.40.6;echo-unsafe",
			wantError:   "release tag must be an explicit v-prefixed semantic version",
		},
		{
			name:        "dispatch fails when tag does not exist",
			eventName:   "workflow_dispatch",
			dispatchTag: "v0.40.6",
			failCommand: "api",
			wantCalls:   []string{"api --silent repos/cameronsjo/bosun/git/ref/tags/v0.40.6"},
			wantError:   "exit status 1",
		},
		{
			name:        "dispatch fails when release does not exist",
			eventName:   "workflow_dispatch",
			dispatchTag: "v0.40.6",
			failCommand: "release",
			wantCalls: []string{
				"api --silent repos/cameronsjo/bosun/git/ref/tags/v0.40.6",
				"release view v0.40.6 --repo cameronsjo/bosun",
			},
			wantError: "exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runResolverScript(t, resolverScript, tt.eventName, tt.dispatchTag, tt.releasePleaseTag, tt.failCommand)
			if tt.wantError == "" {
				require.NoError(t, result.err, result.output)
				assert.Equal(t, "tag_name="+tt.wantTag+"\n", result.githubOutput)
			} else {
				require.Error(t, result.err)
				assert.Contains(t, result.output+result.err.Error(), tt.wantError)
				assert.Empty(t, result.githubOutput)
			}
			assert.Equal(t, tt.wantCalls, result.ghCalls)
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

func loadResolverScript(t *testing.T) string {
	t.Helper()
	parsed, err := parseWorkflow(loadReleasePleaseWorkflow(t))
	require.NoError(t, err)
	resolverJob, ok := parsed.Jobs[resolverJobName]
	require.True(t, ok)
	resolverStep, err := exactlyOneNamedStep(resolverJob.Steps, "Resolve release tag")
	require.NoError(t, err)
	return resolverStep.Run
}

type resolverResult struct {
	err          error
	output       string
	githubOutput string
	ghCalls      []string
}

func runResolverScript(t *testing.T, script, eventName, dispatchTag, releasePleaseTag, failCommand string) resolverResult {
	t.Helper()
	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "bin")
	require.NoError(t, os.Mkdir(fakeBin, 0o700))
	ghPath := filepath.Join(fakeBin, "gh")
	ghScript := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_CALLS_FILE"
if [ "${GH_FAIL_COMMAND:-}" = "$1" ]; then
	exit 1
fi
`
	require.NoError(t, os.WriteFile(ghPath, []byte(ghScript), 0o700))

	githubOutputPath := filepath.Join(testDir, "github-output")
	ghCallsPath := filepath.Join(testDir, "gh-calls")
	command := exec.Command("bash", "-c", script)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EVENT_NAME="+eventName,
		"DISPATCH_TAG="+dispatchTag,
		"RELEASE_PLEASE_TAG="+releasePleaseTag,
		"GH_TOKEN=test-token",
		"GH_REPO=cameronsjo/bosun",
		"GITHUB_OUTPUT="+githubOutputPath,
		"GH_CALLS_FILE="+ghCallsPath,
		"GH_FAIL_COMMAND="+failCommand,
	)
	combinedOutput, commandErr := command.CombinedOutput()
	return resolverResult{
		err:          commandErr,
		output:       string(combinedOutput),
		githubOutput: readOptionalFile(t, githubOutputPath),
		ghCalls:      readOptionalLines(t, ghCallsPath),
	}
}

func readOptionalFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(contents)
}

func readOptionalLines(t *testing.T, path string) []string {
	t.Helper()
	contents := strings.TrimSpace(readOptionalFile(t, path))
	if contents == "" {
		return nil
	}
	return strings.Split(contents, "\n")
}
