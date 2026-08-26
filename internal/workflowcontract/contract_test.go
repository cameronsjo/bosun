// Package workflowcontract validates security-sensitive GitHub Actions control
// flow that the YAML parser accepts but generic workflow linters cannot prove.
package workflowcontract

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	releaseJobName   = "release-please"
	resolverJobName  = "resolve-release-tag"
	releasePushIf    = "${{ github.event_name == 'push' }}"
	releasePRIf      = "${{ steps.release.outputs.pr && vars.RELEASE_APP_ID }}"
	missingAppIDIf   = "${{ steps.release.outputs.pr && vars.RELEASE_APP_ID == '' }}"
	tokenSuccessIf   = "${{ steps.release.outputs.pr && steps.app-token.outcome == 'success' }}"
	tokenFailureIf   = "${{ steps.release.outputs.pr && steps.app-token.outcome == 'failure' }}"
	resolverIf       = "${{ always() && (github.event_name == 'workflow_dispatch' || needs.release-please.outputs.release_created == 'true') }}"
	goreleaserIf     = "${{ always() && needs.resolve-release-tag.result == 'success' && needs.resolve-release-tag.outputs.tag_name != '' }}"
	resolvedTag      = "${{ needs.resolve-release-tag.outputs.tag_name }}"
	appToken         = "${{ steps.app-token.outputs.token }}"
	privateKey       = "${{ secrets.RELEASE_APP_PRIVATE_KEY }}"
	releaseTagOutput = "${{ steps.release-tag.outputs.tag_name }}"
)

type workflow struct {
	On   workflowTriggers `yaml:"on"`
	Jobs map[string]job   `yaml:"jobs"`
}

type workflowTriggers struct {
	Push             pushTrigger             `yaml:"push"`
	WorkflowDispatch workflowDispatchTrigger `yaml:"workflow_dispatch"`
}

type pushTrigger struct {
	Branches []string `yaml:"branches"`
}

type workflowDispatchTrigger struct {
	Inputs map[string]workflowInput `yaml:"inputs"`
}

type workflowInput struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
	Default  any    `yaml:"default"`
}

type job struct {
	If      string            `yaml:"if"`
	Needs   any               `yaml:"needs"`
	Outputs map[string]string `yaml:"outputs"`
	Steps   []step            `yaml:"steps"`
}

type step struct {
	Name            string            `yaml:"name"`
	ID              string            `yaml:"id"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
	Shell           string            `yaml:"shell"`
	Uses            string            `yaml:"uses"`
	With            map[string]string `yaml:"with"`
	Env             map[string]string `yaml:"env"`
	Run             string            `yaml:"run"`
}

type mergePath struct {
	jobName string
	step    step
}

func validateReleasePlease(data []byte) error {
	parsed, err := parseWorkflow(data)
	if err != nil {
		return fmt.Errorf("parse release workflow: %w", err)
	}

	releaseJob, ok := parsed.Jobs[releaseJobName]
	if !ok {
		return errors.New("release-please job is missing")
	}

	var problems []error
	if !slicesEqual(parsed.On.Push.Branches, []string{"main"}) {
		problems = append(problems, errors.New("push trigger must remain limited to main"))
	}
	dispatchTag, ok := parsed.On.WorkflowDispatch.Inputs["tag"]
	if !ok {
		problems = append(problems, errors.New("workflow_dispatch must define a tag input"))
	} else {
		if !dispatchTag.Required || dispatchTag.Type != "string" || dispatchTag.Default != nil {
			problems = append(problems, errors.New("workflow_dispatch tag must be a required string without a default"))
		}
	}
	if releaseJob.If != releasePushIf {
		problems = append(problems, errors.New("release-please job must run only for push events"))
	}

	tokenStep, err := exactlyOneNamedStep(releaseJob.Steps, "Generate App Token")
	if err != nil {
		problems = append(problems, err)
	} else {
		if tokenStep.If != releasePRIf {
			problems = append(problems, errors.New("Generate App Token must require a release PR and configured App ID"))
		}
		if tokenStep.ID != "app-token" {
			problems = append(problems, errors.New("Generate App Token must publish outputs as app-token"))
		}
		if !tokenStep.ContinueOnError {
			problems = append(problems, errors.New("Generate App Token must continue on credential errors"))
		}
		if tokenStep.Uses != "actions/create-github-app-token@v3" {
			problems = append(problems, errors.New("Generate App Token must use actions/create-github-app-token@v3"))
		}
		if tokenStep.With["app-id"] != "${{ vars.RELEASE_APP_ID }}" {
			problems = append(problems, errors.New("Generate App Token must use RELEASE_APP_ID"))
		}
		if tokenStep.With["private-key"] != privateKey {
			problems = append(problems, errors.New("Generate App Token must use RELEASE_APP_PRIVATE_KEY"))
		}
	}

	missingConfigStep, err := exactlyOneNamedStep(releaseJob.Steps, "Release App not configured")
	if err != nil {
		problems = append(problems, err)
	} else {
		if missingConfigStep.If != missingAppIDIf {
			problems = append(problems, errors.New("missing-App warning must require a release PR and absent App ID"))
		}
		if !strings.Contains(missingConfigStep.Run, "::warning::") ||
			!strings.Contains(missingConfigStep.Run, "RELEASE_APP_ID") {
			problems = append(problems, errors.New("missing-App warning must identify RELEASE_APP_ID"))
		}
	}

	warningStep, err := exactlyOneNamedStep(releaseJob.Steps, "Release App token unavailable")
	if err != nil {
		problems = append(problems, err)
	} else {
		if warningStep.If != tokenFailureIf {
			problems = append(problems, errors.New("credential warning must require a failed App-token outcome"))
		}
		if !strings.Contains(warningStep.Run, "::warning::") ||
			!strings.Contains(warningStep.Run, "RELEASE_APP_PRIVATE_KEY") {
			problems = append(problems, errors.New("credential warning must identify RELEASE_APP_PRIVATE_KEY"))
		}
	}

	resolverJob, ok := parsed.Jobs[resolverJobName]
	if !ok {
		problems = append(problems, errors.New("resolve-release-tag job is missing"))
	} else {
		if !jobNeedsExactly(resolverJob, releaseJobName) {
			problems = append(problems, errors.New("resolve-release-tag must need release-please"))
		}
		if resolverJob.If != resolverIf {
			problems = append(problems, errors.New("resolve-release-tag must safely admit dispatch or a created release"))
		}
		if len(resolverJob.Outputs) != 1 || resolverJob.Outputs["tag_name"] != releaseTagOutput {
			problems = append(problems, errors.New("resolve-release-tag must publish one tag_name output"))
		}
		resolverStep, stepErr := exactlyOneNamedStep(resolverJob.Steps, "Resolve release tag")
		if stepErr != nil {
			problems = append(problems, stepErr)
		} else {
			if resolverStep.ID != "release-tag" || resolverStep.Shell != "bash" {
				problems = append(problems, errors.New("Resolve release tag must use the release-tag Bash step"))
			}
			if resolverStep.Env["EVENT_NAME"] != "${{ github.event_name }}" ||
				resolverStep.Env["DISPATCH_TAG"] != "${{ inputs.tag }}" ||
				resolverStep.Env["RELEASE_PLEASE_TAG"] != "${{ needs.release-please.outputs.tag_name }}" {
				problems = append(problems, errors.New("Resolve release tag must receive both push and dispatch tag sources"))
			}
		}
	}

	goreleaserJob, ok := parsed.Jobs["goreleaser"]
	if !ok {
		problems = append(problems, errors.New("goreleaser job is missing"))
	} else {
		if !jobNeedsExactly(goreleaserJob, releaseJobName, resolverJobName) {
			problems = append(problems, errors.New("goreleaser must need release-please and resolve-release-tag"))
		}
		if goreleaserJob.If != goreleaserIf {
			problems = append(problems, errors.New("goreleaser must require a successful non-empty resolved tag"))
		}
		checkoutStep, checkoutErr := exactlyOneStepUsing(goreleaserJob.Steps, "actions/checkout@v4")
		if checkoutErr != nil {
			problems = append(problems, checkoutErr)
		} else if checkoutStep.With["ref"] != resolvedTag {
			problems = append(problems, errors.New("goreleaser checkout must use the resolved tag"))
		}
		versionStep, versionErr := exactlyOneNamedStep(goreleaserJob.Steps, "Extract version")
		if versionErr != nil {
			problems = append(problems, versionErr)
		} else if versionStep.Env["TAG_NAME"] != resolvedTag {
			problems = append(problems, errors.New("version extraction must use the resolved tag"))
		}
		if jobReferences(goreleaserJob, "needs.release-please.outputs.tag_name") {
			problems = append(problems, errors.New("goreleaser must not bypass the resolved tag output"))
		}
	}

	mergePaths := workflowMergePaths(parsed.Jobs)
	if len(mergePaths) != 1 {
		problems = append(problems, fmt.Errorf("workflow must have exactly one active gh pr merge path, found %d", len(mergePaths)))
	} else {
		mergePath := mergePaths[0]
		mergeStep := mergePath.step
		if mergePath.jobName != releaseJobName {
			problems = append(problems, errors.New("the sole merge path must belong to the release-please job"))
		}
		if mergeStep.Name != "Auto-merge release PR" {
			problems = append(problems, errors.New("the sole merge path must be Auto-merge release PR"))
		}
		if mergeStep.If != tokenSuccessIf {
			problems = append(problems, errors.New("auto-merge must require an exact successful App-token outcome"))
		}
		if mergeStep.Env["GH_TOKEN"] != appToken {
			problems = append(problems, errors.New("auto-merge must use only the generated App token"))
		}
		if stepReferencesDefaultToken(mergeStep) {
			problems = append(problems, errors.New("auto-merge must not reference GITHUB_TOKEN"))
		}
	}

	return errors.Join(problems...)
}

func parseWorkflow(data []byte) (workflow, error) {
	var parsed workflow
	err := yaml.Unmarshal(data, &parsed)
	return parsed, err
}

func exactlyOneNamedStep(steps []step, name string) (step, error) {
	var matches []step
	for _, candidate := range steps {
		if candidate.Name == name {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return step{}, fmt.Errorf("expected exactly one %q step, found %d", name, len(matches))
	}
	return matches[0], nil
}

func exactlyOneStepUsing(steps []step, action string) (step, error) {
	var matches []step
	for _, candidate := range steps {
		if candidate.Uses == action {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return step{}, fmt.Errorf("expected exactly one %q action, found %d", action, len(matches))
	}
	return matches[0], nil
}

func jobNeedsExactly(candidate job, expected ...string) bool {
	var needs []string
	switch parsed := candidate.Needs.(type) {
	case string:
		needs = []string{parsed}
	case []any:
		for _, value := range parsed {
			stringValue, ok := value.(string)
			if !ok {
				return false
			}
			needs = append(needs, stringValue)
		}
	case []string:
		needs = parsed
	default:
		return false
	}
	return sameStringSet(needs, expected)
}

func sameStringSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	counts := make(map[string]int, len(actual))
	for _, value := range actual {
		counts[value]++
	}
	for _, value := range expected {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func slicesEqual(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func jobReferences(candidate job, needle string) bool {
	values := []string{candidate.If}
	for _, value := range candidate.Outputs {
		values = append(values, value)
	}
	for _, candidateStep := range candidate.Steps {
		values = append(values, candidateStep.If, candidateStep.Run)
		for _, value := range candidateStep.With {
			values = append(values, value)
		}
		for _, value := range candidateStep.Env {
			values = append(values, value)
		}
	}
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func workflowMergePaths(jobs map[string]job) []mergePath {
	var paths []mergePath
	for jobName, candidateJob := range jobs {
		for _, candidateStep := range candidateJob.Steps {
			for _, line := range activeShellLines(candidateStep.Run) {
				if lineRunsGHPRMerge(line) {
					paths = append(paths, mergePath{jobName: jobName, step: candidateStep})
				}
			}
		}
	}
	return paths
}

func activeShellLines(run string) []string {
	var active []string
	for _, line := range strings.Split(run, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		active = append(active, trimmed)
	}
	return active
}

func lineRunsGHPRMerge(line string) bool {
	fields := strings.Fields(line)
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	if len(fields) > 0 && fields[0] == "command" {
		fields = fields[1:]
	}
	return len(fields) >= 3 && fields[0] == "gh" && fields[1] == "pr" && fields[2] == "merge"
}

func stepReferencesDefaultToken(candidate step) bool {
	values := []string{candidate.If, strings.Join(activeShellLines(candidate.Run), "\n")}
	for _, value := range candidate.With {
		values = append(values, value)
	}
	for _, value := range candidate.Env {
		values = append(values, value)
	}
	for _, value := range values {
		if strings.Contains(value, "secrets.GITHUB_TOKEN") ||
			strings.Contains(value, "github.token") ||
			strings.Contains(value, "$GITHUB_TOKEN") ||
			strings.Contains(value, "${GITHUB_TOKEN}") {
			return true
		}
	}
	return false
}
