package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectFixture is two live compose files under /appdata/compose: a.yml
// declares svc-a (reading an env_file under appdata), b.yml declares svc-b.
func selectFixture() composeSelectionInput {
	return composeSelectionInput{
		Files: []composeFileInputs{
			{Path: "/appdata/compose/a.yml", Services: []string{"svc-a"}, Inputs: []string{"/appdata/svc-a/app.env"}},
			{Path: "/appdata/compose/b.yml", Services: []string{"svc-b"}},
		},
		ComposeDir:         "/appdata/compose",
		ComposeDirEntries:  []string{"/appdata/compose/a.yml", "/appdata/compose/b.yml"},
		ComposeDirListed:   true,
		PrevUnmanaged:      []string{},
		PrevUnmanagedKnown: true,
		Managed:            map[string]bool{"/appdata/compose/a.yml": true, "/appdata/compose/b.yml": true, "/appdata/svc-a/app.env": true},
		LiveKnown:          true,
		Present:            map[string]bool{"svc-a": true, "svc-b": true},
		Containers:         map[string]bool{"svc-a": true, "svc-b": true},
		PrevDeclared:       []string{"svc-a", "svc-b"},
	}
}

func TestSelectComposeFiles(t *testing.T) {
	allFiles := []string{"/appdata/compose/a.yml", "/appdata/compose/b.yml"}

	tests := []struct {
		name       string
		mutate     func(in *composeSelectionInput)
		wantFiles  []string
		wantAll    bool
		wantReason string // substring of Reason (full runs) or of the one selected file's reason
	}{
		{
			name:      "nothing changed and everything running selects nothing",
			mutate:    func(*composeSelectionInput) {},
			wantFiles: []string{},
		},
		{
			name:       "one written compose file selects only that file",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/compose/b.yml"} },
			wantFiles:  []string{"/appdata/compose/b.yml"},
			wantReason: "rendered content changed",
		},
		{
			name:      "an unrelated appdata write selects nothing",
			mutate:    func(in *composeSelectionInput) { in.Changed = []string{"/appdata/traefik/dynamic.yml"} },
			wantFiles: []string{},
		},
		{
			name:       "a written env_file selects the file that reads it",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/svc-a/app.env"} },
			wantFiles:  []string{"/appdata/compose/a.yml"},
			wantReason: "input /appdata/svc-a/app.env changed",
		},
		{
			name:       "a deleted env_file selects the file that reads it",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/svc-a"} },
			wantFiles:  []string{"/appdata/compose/a.yml"},
			wantReason: "changed",
		},
		{
			name: "an opaque file is always selected",
			mutate: func(in *composeSelectionInput) {
				in.Files[1].Opaque = true
				in.Files[1].OpaqueReason = "uses include"
			},
			wantFiles:  []string{"/appdata/compose/b.yml"},
			wantReason: "uses include",
		},
		{
			name:       "a service with no running container selects its file",
			mutate:     func(in *composeSelectionInput) { delete(in.Present, "svc-b") },
			wantFiles:  []string{"/appdata/compose/b.yml"},
			wantReason: "svc-b has no present container",
		},
		{
			name:       "an input bosun did not deploy selects the file that reads it",
			mutate:     func(in *composeSelectionInput) { delete(in.Managed, "/appdata/svc-a/app.env") },
			wantFiles:  []string{"/appdata/compose/a.yml"},
			wantReason: "not deployed by bosun",
		},
		{
			name: "per-file rules selecting every file make it a full run",
			mutate: func(in *composeSelectionInput) {
				in.Changed = []string{"/appdata/compose/a.yml", "/appdata/compose/b.yml"}
			},
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "every file selected",
		},
		{
			name:       "a stopped orphan container runs every file",
			mutate:     func(in *composeSelectionInput) { in.Containers["svc-stopped-orphan"] = true },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "svc-stopped-orphan",
		},
		{
			name:       "an unlisted compose directory runs every file",
			mutate:     func(in *composeSelectionInput) { in.ComposeDirListed = false },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "could not be listed",
		},
		{
			name: "an unmanaged compose directory entry runs every file",
			mutate: func(in *composeSelectionInput) {
				in.ComposeDirEntries = append(in.ComposeDirEntries, "/appdata/compose/.env")
			},
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: `".env" is not deployed by bosun`,
		},
		{
			name: "an unmanaged compose file runs every file",
			mutate: func(in *composeSelectionInput) {
				delete(in.Managed, "/appdata/compose/b.yml")
			},
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: `"b.yml" is not deployed by bosun`,
		},
		{
			name: "a subdirectory holding managed files is managed",
			mutate: func(in *composeSelectionInput) {
				in.ComposeDirEntries = append(in.ComposeDirEntries, "/appdata/compose/fragments")
				in.Managed["/appdata/compose/fragments/x.yml"] = true
			},
			wantFiles: []string{},
		},
		{
			name:       "no record of the last deploy's unmanaged entries runs every file",
			mutate:     func(in *composeSelectionInput) { in.PrevUnmanagedKnown = false; in.PrevUnmanaged = nil },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "no record of unmanaged compose directory entries",
		},
		{
			name:       "an unmanaged entry gone since the last deploy runs every file",
			mutate:     func(in *composeSelectionInput) { in.PrevUnmanaged = []string{".env"} },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "changed since the last deploy (was .env)",
		},
		{
			name:       "a caller fallback reason runs every file",
			mutate:     func(in *composeSelectionInput) { in.FallbackReason = "force" },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "force",
		},
		{
			name:       "unknown live state runs every file",
			mutate:     func(in *composeSelectionInput) { in.LiveKnown = false; in.Present = nil; in.Containers = nil },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "live container state unknown",
		},
		{
			name:       "a changed .env in the compose dir runs every file",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/compose/.env"} },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: ".env",
		},
		{
			name:       "a deleted compose file runs every file",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/compose/gone.yml"} },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "gone.yml",
		},
		{
			name:       "a new compose subdirectory runs every file",
			mutate:     func(in *composeSelectionInput) { in.Changed = []string{"/appdata/compose/fragments"} },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "fragments",
		},
		{
			name: "an unparseable compose file runs every file",
			mutate: func(in *composeSelectionInput) {
				in.Files[0].Err = assert.AnError
			},
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "cannot inspect a.yml",
		},
		{
			name:       "a service removed since the last deploy runs every file",
			mutate:     func(in *composeSelectionInput) { in.PrevDeclared = append(in.PrevDeclared, "svc-old") },
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: `service "svc-old" was removed`,
		},
		{
			name: "an orphan container runs every file so the orphan pass cleans it",
			mutate: func(in *composeSelectionInput) {
				in.Present["svc-orphan"] = true
				in.Containers["svc-orphan"] = true
			},
			wantFiles:  allFiles,
			wantAll:    true,
			wantReason: "svc-orphan",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := selectFixture()
			tc.mutate(&in)
			got := selectComposeFiles(in)
			assert.Equal(t, tc.wantFiles, got.Files)
			assert.Equal(t, tc.wantAll, got.All)
			if tc.wantAll {
				assert.Contains(t, got.Reason, tc.wantReason)
			} else if len(tc.wantFiles) == 1 {
				assert.Contains(t, got.Reasons[tc.wantFiles[0]], tc.wantReason)
			}
		})
	}
}

func TestComposeFullRunReason(t *testing.T) {
	tests := []struct {
		name                                   string
		force, selfHeal, needsRedeploy, hashOn bool
		lastCommit                             string
		managed                                int
		want                                   string
	}{
		{name: "steady state narrows", hashOn: true, lastCommit: "abc", managed: 3, want: ""},
		{name: "force", force: true, hashOn: true, lastCommit: "abc", managed: 3, want: "force"},
		{name: "drift self-heal", selfHeal: true, hashOn: true, lastCommit: "abc", managed: 3, want: "drift self-heal"},
		{name: "incomplete previous deploy", needsRedeploy: true, hashOn: true, lastCommit: "abc", managed: 3, want: "previous deploy did not complete"},
		{name: "first deploy", hashOn: true, managed: 3, want: "no previous successful deploy"},
		{name: "lost manifest", hashOn: true, lastCommit: "abc", want: "no deploy manifest from the previous deploy"},
		{name: "content-hash sync off", lastCommit: "abc", managed: 3, want: "content-hash sync is off"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := composeFullRunReason(tc.force, tc.selfHeal, tc.needsRedeploy, tc.lastCommit, tc.managed, tc.hashOn)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestInspectComposeFile(t *testing.T) {
	appdata := t.TempDir()
	composeDir := filepath.Join(appdata, "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0o755))
	write := func(name, body string) string {
		p := filepath.Join(composeDir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}

	t.Run("services and in-tree inputs", func(t *testing.T) {
		p := write("ok.yml", `
services:
  web:
    image: nginx
    env_file:
      - ../web/app.env
      - path: ../web/extra.env
        required: false
  worker:
    image: busybox
    env_file: ../worker/w.env
    label_file: ../worker/labels
configs:
  cfg:
    file: ../web/cfg.toml
  ext:
    external: true
secrets:
  token:
    file: ../web/token
`)
		got := inspectComposeFile(p, appdata)
		require.NoError(t, got.Err)
		assert.False(t, got.Opaque, got.OpaqueReason)
		assert.Equal(t, []string{"web", "worker"}, got.Services)
		assert.Equal(t, []string{
			filepath.Join(appdata, "web", "app.env"),
			filepath.Join(appdata, "web", "cfg.toml"),
			filepath.Join(appdata, "web", "extra.env"),
			filepath.Join(appdata, "web", "token"),
			filepath.Join(appdata, "worker", "labels"),
			filepath.Join(appdata, "worker", "w.env"),
		}, got.Inputs)
	})

	opaqueCases := map[string]string{
		"include":       "include:\n  - other.yml\nservices:\n  a:\n    image: x\n",
		"extends file":  "services:\n  a:\n    image: x\n    extends:\n      file: base.yml\n      service: b\n",
		"absolute path": "services:\n  a:\n    image: x\n    env_file: /etc/elsewhere.env\n",
		"escaping path": "services:\n  a:\n    image: x\n    env_file: ../../outside.env\n",
		"interpolated":  "services:\n  a:\n    image: x\n    env_file: ${ENV_DIR}/a.env\n",
		"bad env shape": "services:\n  a:\n    image: x\n    env_file: 3\n",
		"pull always":   "services:\n  a:\n    image: x\n    pull_policy: always\n",
		"pull daily":    "services:\n  a:\n    image: x\n    pull_policy: daily\n",
		"pull build":    "services:\n  a:\n    image: x\n    pull_policy: build\n",
		"scale 3":       "services:\n  a:\n    image: x\n    scale: 3\n",
		"replicas 2":    "services:\n  a:\n    image: x\n    deploy:\n      replicas: 2\n",
		"replicas var":  "services:\n  a:\n    image: x\n    deploy:\n      replicas: ${N}\n",
	}
	for name, body := range opaqueCases {
		t.Run("opaque: "+name, func(t *testing.T) {
			got := inspectComposeFile(write("opaque.yml", body), appdata)
			require.NoError(t, got.Err)
			assert.True(t, got.Opaque, "expected %s to be opaque", name)
			assert.NotEmpty(t, got.OpaqueReason)
		})
	}

	for _, policy := range []string{"missing", "if_not_present", "never"} {
		t.Run("pull_policy "+policy+" is not opaque", func(t *testing.T) {
			got := inspectComposeFile(write("pp.yml", "services:\n  a:\n    image: x\n    pull_policy: "+policy+"\n"), appdata)
			require.NoError(t, got.Err)
			assert.False(t, got.Opaque, got.OpaqueReason)
		})
	}

	t.Run("one replica is not opaque", func(t *testing.T) {
		got := inspectComposeFile(write("one.yml", "services:\n  a:\n    image: x\n    scale: 1\n    deploy:\n      replicas: 1\n"), appdata)
		require.NoError(t, got.Err)
		assert.False(t, got.Opaque, got.OpaqueReason)
	})

	t.Run("same-file extends is not opaque", func(t *testing.T) {
		got := inspectComposeFile(write("ext.yml", "services:\n  a:\n    image: x\n    extends: b\n  b:\n    image: y\n"), appdata)
		require.NoError(t, got.Err)
		assert.False(t, got.Opaque)
	})

	t.Run("invalid YAML is an error", func(t *testing.T) {
		got := inspectComposeFile(write("bad.yml", "services: [unclosed\n"), appdata)
		assert.Error(t, got.Err)
	})

	t.Run("services not a mapping is an error", func(t *testing.T) {
		got := inspectComposeFile(write("list.yml", "services:\n  - a\n"), appdata)
		assert.Error(t, got.Err)
	})

	t.Run("missing file is an error", func(t *testing.T) {
		got := inspectComposeFile(filepath.Join(composeDir, "nope.yml"), appdata)
		assert.Error(t, got.Err)
	})
}

func TestStagingRelToLive(t *testing.T) {
	targets := []DeployTarget{
		{RelPath: "compose", TargetPath: "compose", IsDir: true},
		{RelPath: filepath.Join("appdata", "traefik"), TargetPath: "traefik", IsDir: true},
		{RelPath: filepath.Join("appdata", "single.conf"), TargetPath: "single.conf"},
	}
	tests := []struct {
		rel    string
		want   string
		wantOK bool
	}{
		{rel: filepath.Join("compose", "a.yml"), want: filepath.Join("/ad", "compose", "a.yml"), wantOK: true},
		{rel: filepath.Join("appdata", "traefik", "dyn", "x.yml"), want: filepath.Join("/ad", "traefik", "dyn", "x.yml"), wantOK: true},
		{rel: filepath.Join("appdata", "traefik"), want: filepath.Join("/ad", "traefik"), wantOK: true},
		{rel: filepath.Join("appdata", "single.conf"), want: filepath.Join("/ad", "single.conf"), wantOK: true},
		{rel: filepath.Join("appdata", "traefik2", "x"), wantOK: false},
		{rel: "elsewhere", wantOK: false},
	}
	for _, tc := range tests {
		got, ok := stagingRelToLive(targets, "/ad", tc.rel)
		assert.Equal(t, tc.wantOK, ok, tc.rel)
		if tc.wantOK {
			assert.Equal(t, tc.want, got, tc.rel)
		}
	}
}

// changedOnlyHarness builds a Run() harness for #726: two compose files in
// the repo (a.yml declares svc-a, b.yml declares svc-b), a live appdata tree
// seeded from liveA/liveB, a prior successful deploy in state, and a docker
// mock reporting the given running services. composeUpFn records the files
// phase 1 ran; a docker shim on PATH logs every shelled-out docker call so the
// orphan pass (which shells out) is observable.
func changedOnlyHarness(t *testing.T, liveB string, prior DeployState, running []string) (r *Reconciler, upCalls *[][]string, dockerLog string, appdata string) {
	t.Helper()
	return changedOnlyHarnessWith(t, harnessOpts{liveB: liveB, prior: prior, running: running})
}

// harnessOpts extends changedOnlyHarness. fileA overrides a.yml's content (in
// both repo and live). repoFiles are written under the repo root and
// liveFiles under appdata, keyed by slash-separated relative path.
type harnessOpts struct {
	liveB     string
	prior     DeployState
	running   []string
	fileA     string
	repoFiles map[string]string
	liveFiles map[string]string
	// composeIgnore sets Config.ComposeIgnore (compose_ignore, #744).
	composeIgnore []string
	// syncExclude sets Config.DeploySyncExclude.
	syncExclude []string
}

func changedOnlyHarnessWith(t *testing.T, o harnessOpts) (r *Reconciler, upCalls *[][]string, dockerLog string, appdata string) {
	t.Helper()
	liveB, prior, running := o.liveB, o.prior, o.running
	tmp := t.TempDir()
	shimDir := t.TempDir()
	dockerLog = filepath.Join(tmp, "docker.log")
	shim := "#!/bin/sh\necho \"$@\" >> '" + dockerLog + "'\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(shimDir, "docker"), []byte(shim), 0o755))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stateFile := filepath.Join(tmp, "state.json")
	repoDir := filepath.Join(tmp, "repo")
	appdata = filepath.Join(tmp, "appdata")
	fileA := "services:\n  svc-a:\n    image: alpine:1\n"
	if o.fileA != "" {
		fileA = o.fileA
	}
	const fileB = "services:\n  svc-b:\n    image: alpine:2\n"
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "compose", "a.yml"), []byte(fileA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "compose", "b.yml"), []byte(fileB), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(appdata, "compose"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "compose", "a.yml"), []byte(fileA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appdata, "compose", "b.yml"), []byte(liveB), 0o644))
	writeTree := func(root string, files map[string]string) {
		for rel, body := range files {
			p := filepath.Join(root, filepath.FromSlash(rel))
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		}
	}
	writeTree(repoDir, o.repoFiles)
	writeTree(appdata, o.liveFiles)

	prior.SchemaVersion = 2
	require.NoError(t, SaveState(stateFile, &prior))

	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		var items []container.Summary
		for _, entry := range running {
			// "name" means running; "name=state" sets another container state.
			svc, state, found := strings.Cut(entry, "=")
			if !found {
				state = "running"
			}
			items = append(items, container.Summary{
				ID:     "id-" + svc + "-0000000000",
				Names:  []string{"/test-" + svc + "-1"},
				Image:  "alpine",
				State:  container.ContainerState(state),
				Labels: map[string]string{"com.docker.compose.project": "test", "com.docker.compose.service": svc},
			})
		}
		return client.ContainerListResult{Items: items}, nil
	}

	calls := [][]string{}
	upCalls = &calls
	deploy := &DeployOps{
		ProjectName:     "test",
		ContentHashSync: true,
		RemoveOrphans:   true,
		composeUpFn: func(_ context.Context, files []string) error {
			*upCalls = append(*upCalls, files)
			return nil
		},
	}
	cfg := &Config{
		LockFile:           privateLockPath(t, tmp, "reconcile.lock"),
		StateFile:          stateFile,
		RepoDir:            repoDir,
		StagingDir:         filepath.Join(tmp, "staging"),
		BackupDir:          filepath.Join(tmp, "backups"),
		LocalAppdataPath:   appdata,
		InfraSubDir:        ".",
		ContentHashSync:    true,
		HealthCheckTimeout: 0,
	}
	cfg.ComposeIgnore.Value = o.composeIgnore
	cfg.DeploySyncExclude.Value = o.syncExclude
	gitOps := &mockGitOps{syncChanged: true, syncBefore: "prevcommit", syncAfter: "newcommit"}
	r = NewReconciler(cfg, WithGitOperations(gitOps), WithDeployOps(deploy), WithDockerClient(docker.NewClientWithAPI(mockAPI)))
	return r, upCalls, dockerLog, appdata
}

func steadyPrior() DeployState {
	return DeployState{
		ComposeUnmanagedRecorded: true,
		LastDeployedCommit:       "prevcommit",
		DeployedFiles:            []string{"compose/a.yml", "compose/b.yml"},
		DeclaredServices:         []DeclaredService{{Name: "svc-a"}, {Name: "svc-b"}},
	}
}

func readDockerLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func upFileBases(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		for _, f := range c {
			out = append(out, filepath.Base(f))
		}
	}
	return out
}

func TestRun_ChangedOnlyComposeUp_RunsOnlyTheChangedFile(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:OLD\n", steadyPrior(), []string{"svc-a", "svc-b"})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"b.yml"}, upFileBases(*calls), "only the file whose rendered content changed may run compose up")
	assert.NotContains(t, readDockerLog(t, dockerLog), "--remove-orphans",
		"the orphan pass must not run over a subset: it would remove a.yml's services")
	assert.Len(t, r.lastComposeFiles, 2, "health-gate rollback scope must stay the full file list")
}

func TestRun_ChangedOnlyComposeUp_NothingChangedSkipsComposeUp(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:2\n", steadyPrior(), []string{"svc-a", "svc-b"})

	require.NoError(t, r.Run(context.Background()))

	assert.Empty(t, *calls)
	assert.NotContains(t, readDockerLog(t, dockerLog), "--remove-orphans")
}

func TestRun_ChangedOnlyComposeUp_NotRunningServiceSelectsItsFile(t *testing.T) {
	r, calls, _, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:2\n", steadyPrior(), []string{"svc-b"})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml"}, upFileBases(*calls))
}

func TestRun_ChangedOnlyComposeUp_FallsBackToEveryFile(t *testing.T) {
	changedB := "services:\n  svc-b:\n    image: alpine:OLD\n"
	tests := []struct {
		name    string
		prior   func() DeployState
		running []string
	}{
		{
			name:    "previous deploy left NeedsRedeploy set",
			prior:   func() DeployState { s := steadyPrior(); s.NeedsRedeploy = true; return s },
			running: []string{"svc-a", "svc-b"},
		},
		{
			name:    "no previous successful deploy",
			prior:   func() DeployState { s := steadyPrior(); s.LastDeployedCommit = ""; return s },
			running: []string{"svc-a", "svc-b"},
		},
		{
			name:    "lost deploy manifest",
			prior:   func() DeployState { s := steadyPrior(); s.DeployedFiles = nil; return s },
			running: []string{"svc-a", "svc-b"},
		},
		{
			name:    "orphan container present",
			prior:   steadyPrior,
			running: []string{"svc-a", "svc-b", "svc-gone"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, calls, dockerLog, _ := changedOnlyHarness(t, changedB, tc.prior(), tc.running)

			_ = r.Run(context.Background()) // outcome beyond compose selection is not under test

			assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls))
			assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans", "a full run keeps the orphan pass")
		})
	}
}

func TestPresentServices(t *testing.T) {
	actual := []ActualService{
		{Name: "up", State: "running"},
		{Name: "looping", State: "restarting"},
		{Name: "stopped", State: "exited"},
		{Name: "breaker", State: "exited"},
		{Name: "fresh", State: "created"},
		{Name: "dead", State: "dead"},
	}
	tracking := map[string]RestartTrackingEntry{"breaker": {Tripped: true}}
	got := presentServices(actual, tracking)
	assert.Equal(t, map[string]bool{"up": true, "looping": true, "breaker": true}, got,
		"presence must follow ServiceIsPresent: running, restarting, or breaker-tripped")
	for _, svc := range actual {
		assert.Equal(t, ServiceIsPresent(svc.State, tracking[svc.Name].Tripped), got[svc.Name], svc.Name)
	}
	assert.Equal(t, map[string]bool{"up": true, "looping": true, "stopped": true, "breaker": true, "fresh": true, "dead": true},
		containerServices(actual), "orphan detection counts containers in every state")
}

func TestRun_ChangedOnlyComposeUp_CrashLoopingServiceIsNotReapplied(t *testing.T) {
	r, calls, _, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:2\n", steadyPrior(), []string{"svc-a=restarting", "svc-b"})

	_ = r.Run(context.Background()) // post-deploy outcome for a crash loop is not under test

	assert.Empty(t, *calls, "compose up cannot fix a crash loop; its unchanged file must not run every deploy")
}

func TestRun_ChangedOnlyComposeUp_ExitedServiceSelectsItsFile(t *testing.T) {
	r, calls, _, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:2\n", steadyPrior(), []string{"svc-a=exited", "svc-b"})

	_ = r.Run(context.Background())

	assert.Equal(t, []string{"a.yml"}, upFileBases(*calls))
}

// envFileA is a.yml reading an env_file from its own appdata target, so the
// pipeline must map content-hash sync's staging-relative write
// ("appdata/svc-a/app.env") to the live path the compose file resolves
// ("<appdata>/svc-a/app.env").
const envFileA = "services:\n  svc-a:\n    image: alpine:1\n    env_file: ../svc-a/app.env\n"

func envFilePrior() DeployState {
	s := steadyPrior()
	s.DeployedFiles = append(s.DeployedFiles, "svc-a/app.env", "svc-a/keep.conf")
	return s
}

func TestRun_ChangedOnlyComposeUp_ChangedEnvFileSelectsItsComposeFile(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:   "services:\n  svc-b:\n    image: alpine:2\n",
		prior:   envFilePrior(),
		running: []string{"svc-a", "svc-b"},
		fileA:   envFileA,
		repoFiles: map[string]string{
			"appdata/svc-a/app.env":   "LEVEL=new\n",
			"appdata/svc-a/keep.conf": "same\n",
		},
		liveFiles: map[string]string{
			"svc-a/app.env":   "LEVEL=old\n",
			"svc-a/keep.conf": "same\n",
		},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml"}, upFileBases(*calls), "an env_file change must re-apply the compose file that reads it, and only that one")
	assert.NotContains(t, readDockerLog(t, dockerLog), "--remove-orphans")
}

func TestRun_ChangedOnlyComposeUp_UnchangedEnvFileSelectsNothing(t *testing.T) {
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:   "services:\n  svc-b:\n    image: alpine:2\n",
		prior:   envFilePrior(),
		running: []string{"svc-a", "svc-b"},
		fileA:   envFileA,
		repoFiles: map[string]string{
			"appdata/svc-a/app.env":   "LEVEL=same\n",
			"appdata/svc-a/keep.conf": "same\n",
		},
		liveFiles: map[string]string{
			"svc-a/app.env":   "LEVEL=same\n",
			"svc-a/keep.conf": "same\n",
		},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Empty(t, *calls, "a managed, unchanged env_file is tracked and must not force a.yml")
}

// A deleted input is never in this run's manifest, so the unmanaged-input
// rule selects a.yml before the deleted-path mapping is consulted; this pins
// the behavior (the file runs), not the DeletedFiles mapping. The changed
// env_file test above pins the mapping.
func TestRun_ChangedOnlyComposeUp_DeletedEnvFileSelectsItsComposeFile(t *testing.T) {
	r, calls, _, appdata := changedOnlyHarnessWith(t, harnessOpts{
		liveB:   "services:\n  svc-b:\n    image: alpine:2\n",
		prior:   envFilePrior(),
		running: []string{"svc-a", "svc-b"},
		fileA:   envFileA,
		repoFiles: map[string]string{
			"appdata/svc-a/keep.conf": "same\n",
		},
		liveFiles: map[string]string{
			"svc-a/app.env":   "LEVEL=old\n",
			"svc-a/keep.conf": "same\n",
		},
	})

	_ = r.Run(context.Background()) // compose itself would reject the missing env_file; selection is under test

	_, statErr := os.Stat(filepath.Join(appdata, "svc-a", "app.env"))
	require.True(t, os.IsNotExist(statErr), "precondition: the stale env_file was pruned")
	assert.Equal(t, []string{"a.yml"}, upFileBases(*calls))
}

// Compose-directory entries an operator creates on the
// host. Bosun never deploys them, so it cannot see their changes, and compose
// still reads them (.env for interpolation, every *.yml through the glob).
// Each must force a full run.
func TestRun_ChangedOnlyComposeUp_UnmanagedComposeDirEntryRunsEveryFile(t *testing.T) {
	tests := []struct {
		name      string
		fileA     string
		liveFiles map[string]string
		withSvcC  bool // svc-c is declared (by c.yml) and running
		wantFiles []string
	}{
		{
			name:      "host-created .env used for interpolation",
			fileA:     "services:\n  svc-a:\n    image: alpine:${TAG}\n",
			liveFiles: map[string]string{"compose/.env": "TAG=2\n"},
			wantFiles: []string{"a.yml", "b.yml"},
		},
		{
			name: "host-created compose file",
			liveFiles: map[string]string{
				"compose/c.yml": "services:\n  svc-c:\n    image: alpine:3\n",
			},
			withSvcC:  true,
			wantFiles: []string{"a.yml", "b.yml", "c.yml"},
		},
		{
			name:      "stray non-yml file",
			liveFiles: map[string]string{"compose/notes.txt": "hand edit\n"},
			wantFiles: []string{"a.yml", "b.yml"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			running := []string{"svc-a", "svc-b"}
			prior := steadyPrior()
			if tc.withSvcC {
				running = append(running, "svc-c")
				prior.DeclaredServices = append(prior.DeclaredServices, DeclaredService{Name: "svc-c"})
			}
			r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
				liveB:     "services:\n  svc-b:\n    image: alpine:2\n",
				prior:     prior,
				running:   running,
				fileA:     tc.fileA,
				liveFiles: tc.liveFiles,
			})

			_ = r.Run(context.Background()) // outcome beyond compose selection is not under test

			assert.Equal(t, tc.wantFiles, upFileBases(*calls), "nothing changed in bosun's view, but an unmanaged entry must still force every file")
			assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans", "a full run keeps the orphan pass")
		})
	}
}

// A host-deleted compose/.env leaves nothing on disk or in the written list;
// only the last deploy's record of unmanaged entries shows it was there.
func TestRun_ChangedOnlyComposeUp_HostDeletedUnmanagedEntryRunsEveryFile(t *testing.T) {
	prior := steadyPrior()
	prior.ComposeUnmanagedEntries = []string{".env"}
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:   "services:\n  svc-b:\n    image: alpine:2\n",
		prior:   prior,
		running: []string{"svc-a", "svc-b"},
		fileA:   "services:\n  svc-a:\n    image: alpine:${TAG:-1}\n",
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls))
	assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans")
	saved := LoadState(r.config.StateFile)
	assert.True(t, saved.ComposeUnmanagedRecorded)
	assert.Empty(t, saved.ComposeUnmanagedEntries, "the record now says nothing unmanaged remains, so the next deploy can narrow again")
}

// State from an older bosun has no record: fail closed once, then record.
func TestRun_ChangedOnlyComposeUp_MissingUnmanagedRecordRunsEveryFileOnceThenRecords(t *testing.T) {
	prior := steadyPrior()
	prior.ComposeUnmanagedRecorded = false
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:     "services:\n  svc-b:\n    image: alpine:2\n",
		prior:     prior,
		running:   []string{"svc-a", "svc-b"},
		liveFiles: map[string]string{"compose/.env": "TAG=2\n"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls))
	saved := LoadState(r.config.StateFile)
	assert.True(t, saved.ComposeUnmanagedRecorded)
	assert.Equal(t, []string{".env"}, saved.ComposeUnmanagedEntries)
}

// compose_ignore (#744): an operator-listed unmanaged entry neither forces a
// full run nor is read as compose input; an entry not on the list still does.
const ignoreChangedB = "services:\n  svc-b:\n    image: alpine:OLD\n"

func TestRun_ComposeIgnore_IgnoredStrayDirKeepsSubsetRun(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         ignoreChangedB,
		prior:         steadyPrior(),
		running:       []string{"svc-a", "svc-b"},
		liveFiles:     map[string]string{"compose/handmade/compose.yaml": "services:\n  other:\n    image: alpine\n"},
		composeIgnore: []string{"handmade"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"b.yml"}, upFileBases(*calls), "an ignored unmanaged directory must not force every file")
	assert.NotContains(t, readDockerLog(t, dockerLog), "--remove-orphans")
	saved := LoadState(r.config.StateFile)
	assert.True(t, saved.ComposeUnmanagedRecorded)
	assert.Empty(t, saved.ComposeUnmanagedEntries, "ignored entries stay out of the recorded unmanaged list")
}

func TestRun_ComposeIgnore_NonIgnoredStrayDirRunsEveryFile(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         ignoreChangedB,
		prior:         steadyPrior(),
		running:       []string{"svc-a", "svc-b"},
		liveFiles:     map[string]string{"compose/handmade/compose.yaml": "x: 1\n"},
		composeIgnore: []string{"something-else"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "an unmanaged entry not on the list keeps failing closed")
	assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans")
}

// The last deploy ran with "handmade" ignored (empty record); the operator has
// since removed it from the list, so the entry is unmanaged again.
func TestRun_ComposeIgnore_RemovedEntryFailsClosedAgain(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:     ignoreChangedB,
		prior:     steadyPrior(),
		running:   []string{"svc-a", "svc-b"},
		liveFiles: map[string]string{"compose/handmade/compose.yaml": "x: 1\n"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls))
	assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans")
	saved := LoadState(r.config.StateFile)
	assert.Equal(t, []string{"handmade"}, saved.ComposeUnmanagedEntries)
}

// The last deploy recorded "handmade" as unmanaged; adding it to the list
// takes effect at once instead of tripping the changed-record rule.
func TestRun_ComposeIgnore_AddedEntryTakesEffectImmediately(t *testing.T) {
	prior := steadyPrior()
	prior.ComposeUnmanagedEntries = []string{"handmade"}
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         ignoreChangedB,
		prior:         prior,
		running:       []string{"svc-a", "svc-b"},
		liveFiles:     map[string]string{"compose/handmade/compose.yaml": "x: 1\n"},
		composeIgnore: []string{"handmade"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"b.yml"}, upFileBases(*calls))
}

func TestRun_ComposeIgnore_IgnoredYmlFileIsNotRun(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:   "services:\n  svc-b:\n    image: alpine:2\n",
		prior:   steadyPrior(),
		running: []string{"svc-a", "svc-b"},
		// Unparseable on purpose: inspecting it would force a full run.
		liveFiles:     map[string]string{"compose/extra.yml": "services: [not, a, mapping\n"},
		composeIgnore: []string{"extra.yml"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Empty(t, *calls, "an ignored *.yml is neither inspected nor passed to compose up")
	assert.NotContains(t, readDockerLog(t, dockerLog), "extra.yml")
	for _, f := range r.lastComposeFiles {
		assert.NotEqual(t, "extra.yml", filepath.Base(f), "rollback scope excludes ignored files too")
	}
}

// b.yml is in this run's manifest only (the last deploy did not record it),
// so this pins the current-run ownership check.
func TestRun_ComposeIgnore_ManagedEntryIsNeverIgnored(t *testing.T) {
	prior := steadyPrior()
	prior.DeployedFiles = []string{"compose/a.yml"}
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         ignoreChangedB,
		prior:         prior,
		running:       []string{"svc-a", "svc-b"},
		composeIgnore: []string{"b.yml"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"b.yml"}, upFileBases(*calls), "a file bosun deploys stays managed even when listed")
}

// With compose/ excluded from this run's sync, this run's manifest has no
// compose files; the last deploy's manifest still marks b.yml as bosun's.
func TestRun_ComposeIgnore_PreviouslyManagedFileIsNeverIgnored(t *testing.T) {
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         "services:\n  svc-b:\n    image: alpine:2\n",
		prior:         steadyPrior(),
		running:       []string{"svc-a", "svc-b"},
		composeIgnore: []string{"b.yml"},
		syncExclude:   []string{"compose"},
	})

	_ = r.Run(context.Background()) // outcome beyond compose selection is not under test

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "a file the last deploy owned stays managed when compose/ is not synced")
}

func TestRun_ComposeIgnore_InvalidListIgnoresNothing(t *testing.T) {
	r, calls, _, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         ignoreChangedB,
		prior:         steadyPrior(),
		running:       []string{"svc-a", "svc-b"},
		liveFiles:     map[string]string{"compose/handmade/compose.yaml": "x: 1\n"},
		composeIgnore: []string{"handmade", "../escape"},
	})

	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "an invalid list is dropped whole, so selection fails closed")
}

func TestValidateComposeIgnore(t *testing.T) {
	assert.NoError(t, ValidateComposeIgnore(nil))
	assert.NoError(t, ValidateComposeIgnore([]string{"handmade", "notes.txt", ".git", "extra.yml"}))
	bad := map[string]string{
		"empty":       "",
		"dot":         ".",
		"dotdot":      "..",
		"slash":       "a/b",
		"traversal":   "../x",
		"backslash":   `a\b`,
		"star":        "*.yml",
		"question":    "a?.yml",
		"class":       "[ab].yml",
		"whitespace":  " handmade",
		"nul":         "a\x00b",
		"dotenv":      ".env",
		"absolute":    "/etc",
		"trailing ws": "handmade\t",
	}
	for name, v := range bad {
		t.Run(name, func(t *testing.T) {
			err := ValidateComposeIgnore([]string{"ok", v})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "compose_ignore[1]")
		})
	}
}

func TestSelectComposeFiles_ComposeIgnore(t *testing.T) {
	in := selectFixture()
	stray := filepath.Join(in.ComposeDir, "handmade")
	in.ComposeDirEntries = append(in.ComposeDirEntries, stray)

	got := selectComposeFiles(in)
	assert.True(t, got.All, "precondition: the stray entry forces a full run")

	in.Ignored = map[string]bool{"handmade": true}
	got = selectComposeFiles(in)
	assert.False(t, got.All, "an ignored stray entry must not force a full run")
	assert.Equal(t, []string{}, unmanagedComposeEntries(in.ComposeDirEntries, in.Managed, in.Ignored))
}

// Declared contract: an ignored compose file whose stack still runs under
// bosun's project name is no longer declared, so its containers are orphans
// and the next full run removes them.
func TestRun_ComposeIgnore_IgnoredFileUnderBosunProjectIsOrphaned(t *testing.T) {
	r, calls, dockerLog, _ := changedOnlyHarnessWith(t, harnessOpts{
		liveB:         "services:\n  svc-b:\n    image: alpine:2\n",
		prior:         steadyPrior(),
		running:       []string{"svc-a", "svc-b", "svc-x"},
		liveFiles:     map[string]string{"compose/extra.yml": "services:\n  svc-x:\n    image: alpine\n"},
		composeIgnore: []string{"extra.yml"},
	})

	_ = r.Run(context.Background()) // outcome beyond compose selection is not under test

	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "the ignored file is not run, and its container forces a full run")
	assert.Contains(t, readDockerLog(t, dockerLog), "--remove-orphans", "the full run's orphan pass removes the ignored file's containers")
}
