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
	releaseJobName = "release-please"
	releasePRIf    = "${{ steps.release.outputs.pr && vars.RELEASE_APP_ID }}"
	missingAppIDIf = "${{ steps.release.outputs.pr && vars.RELEASE_APP_ID == '' }}"
	tokenSuccessIf = "${{ steps.release.outputs.pr && steps.app-token.outcome == 'success' }}"
	tokenFailureIf = "${{ steps.release.outputs.pr && steps.app-token.outcome == 'failure' }}"
	appToken       = "${{ steps.app-token.outputs.token }}"
	privateKey     = "${{ secrets.RELEASE_APP_PRIVATE_KEY }}"
)

type workflow struct {
	Jobs map[string]job `yaml:"jobs"`
}

type job struct {
	Steps []step `yaml:"steps"`
}

type step struct {
	Name            string            `yaml:"name"`
	ID              string            `yaml:"id"`
	If              string            `yaml:"if"`
	ContinueOnError bool              `yaml:"continue-on-error"`
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
	var parsed workflow
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parse release workflow: %w", err)
	}

	releaseJob, ok := parsed.Jobs[releaseJobName]
	if !ok {
		return errors.New("release-please job is missing")
	}

	var problems []error
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
