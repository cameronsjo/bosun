package workflowcontract

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// pagesWorkflow mirrors the fields the Pages deploy contract asserts on. It is
// deliberately separate from the release-please types: the Pages contract cares
// about environments and concurrency, which those types do not model.
type pagesWorkflow struct {
	On          pagesTriggers        `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Jobs        map[string]*pagesJob `yaml:"jobs"`
}

type pagesTriggers struct {
	Push        *pagesPathTrigger `yaml:"push"`
	PullRequest *pagesPathTrigger `yaml:"pull_request"`
}

type pagesPathTrigger struct {
	Branches []string `yaml:"branches"`
	Paths    []string `yaml:"paths"`
}

type pagesJob struct {
	If          string            `yaml:"if"`
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Environment pagesEnvironment  `yaml:"environment"`
	Concurrency pagesConcurrency  `yaml:"concurrency"`
	Steps       []pagesStep       `yaml:"steps"`
}

type pagesEnvironment struct {
	Name string `yaml:"name"`
}

type pagesConcurrency struct {
	Group            string `yaml:"group"`
	CancelInProgress bool   `yaml:"cancel-in-progress"`
}

type pagesStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
	Run  string            `yaml:"run"`
}

func loadPagesWorkflow(t *testing.T) pagesWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "pages.yml"))
	require.NoError(t, err)
	var parsed pagesWorkflow
	require.NoError(t, yaml.Unmarshal(data, &parsed))
	return parsed
}

func TestPagesWorkflowTriggersAreExactlyMainPushPRAndDispatch(t *testing.T) {
	wf := loadPagesWorkflow(t)

	// The trigger *set* is asserted on raw keys: an empty-bodied trigger like
	// workflow_dispatch unmarshals to nil in a typed struct, which cannot
	// distinguish present from absent.
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "pages.yml"))
	require.NoError(t, err)
	var raw struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	require.NoError(t, yaml.Unmarshal(data, &raw))
	triggers := make([]string, 0, len(raw.On))
	for k := range raw.On {
		triggers = append(triggers, k)
	}
	assert.ElementsMatch(t, []string{"push", "pull_request", "workflow_dispatch"}, triggers,
		"triggers must be exactly main push, pull_request, and workflow_dispatch (rollback path)")

	require.NotNil(t, wf.On.Push, "push trigger must exist")
	assert.Equal(t, []string{"main"}, wf.On.Push.Branches, "push must be scoped to main")
	assert.NotEmpty(t, wf.On.Push.Paths, "push must be path-scoped")

	require.NotNil(t, wf.On.PullRequest, "pull_request trigger must exist")
	assert.Equal(t, []string{"main"}, wf.On.PullRequest.Branches)
	assert.Equal(t, wf.On.Push.Paths, wf.On.PullRequest.Paths,
		"push and pull_request must gate on the same path set")
}

func TestPagesWorkflowDefaultPermissionsAreReadOnly(t *testing.T) {
	wf := loadPagesWorkflow(t)
	assert.Equal(t, map[string]string{"contents": "read"}, wf.Permissions,
		"workflow-level permissions must be exactly contents: read")
}

func TestPagesWorkflowWriteScopesConfinedToDeployJob(t *testing.T) {
	wf := loadPagesWorkflow(t)
	require.Contains(t, wf.Jobs, "build")
	require.Contains(t, wf.Jobs, "deploy")
	assert.Len(t, wf.Jobs, 2, "exactly the build and deploy jobs")

	build := wf.Jobs["build"]
	assert.Empty(t, build.Permissions, "build job must not elevate permissions")

	deploy := wf.Jobs["deploy"]
	assert.Equal(t, map[string]string{"pages": "write", "id-token": "write"},
		deploy.Permissions, "deploy job carries exactly the Pages OIDC write scopes")
}

func TestPagesWorkflowDeployJobIsGatedAndBound(t *testing.T) {
	wf := loadPagesWorkflow(t)
	deploy := wf.Jobs["deploy"]
	require.NotNil(t, deploy)

	assert.Equal(t, "${{ github.event_name != 'pull_request' }}", deploy.If,
		"deploy must be excluded on pull_request builds")
	assert.Equal(t, "build", deploy.Needs)
	assert.Equal(t, "github-pages", deploy.Environment.Name,
		"deploy must bind the github-pages environment (its branch policy is the deploy gate)")
	assert.Equal(t, "pages", deploy.Concurrency.Group)
	assert.False(t, deploy.Concurrency.CancelInProgress,
		"an in-flight deploy must not be cancelled mid-publish")
}

func TestPagesWorkflowArtifactUploadIsBuildOutputAndPRGated(t *testing.T) {
	wf := loadPagesWorkflow(t)
	build := wf.Jobs["build"]
	require.NotNil(t, build)

	var upload *pagesStep
	for i := range build.Steps {
		if regexp.MustCompile(`^actions/upload-pages-artifact@`).MatchString(build.Steps[i].Uses) {
			require.Nil(t, upload, "exactly one upload-pages-artifact step")
			upload = &build.Steps[i]
		}
	}
	require.NotNil(t, upload, "build job must upload the Pages artifact")
	assert.Equal(t, "site/dist", upload.With["path"])
	assert.Equal(t, "${{ github.event_name != 'pull_request' }}", upload.If,
		"pull_request runs are build-only: no artifact upload")
}

func TestPagesWorkflowActionsAreSHAPinned(t *testing.T) {
	wf := loadPagesWorkflow(t)
	shaPin := regexp.MustCompile(`@[0-9a-f]{40}$`)
	pinned := 0
	for jobName, j := range wf.Jobs {
		// A job converted to a reusable-workflow call has no steps, which
		// would make this loop pass vacuously on an unpinned reference.
		require.NotEmpty(t, j.Steps, "job %s must declare steps (no reusable-workflow calls)", jobName)
		for _, s := range j.Steps {
			if s.Uses == "" {
				continue
			}
			assert.Regexp(t, shaPin, s.Uses,
				"job %s step %q must pin its action to a full commit SHA", jobName, s.Name)
			pinned++
		}
	}
	assert.GreaterOrEqual(t, pinned, 5,
		"the workflow is expected to use at least checkout, pnpm, node, upload, and deploy actions")
}
