package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
	"gopkg.in/yaml.v3"
)

// Changed-only compose up (#726).
//
// A local deploy used to run `docker compose up -d` on every live compose
// file, so a commit that touched one stack still put all of them through
// compose. Bosun now runs compose up only on the files whose effective input
// changed this run, and falls back to every file whenever it cannot tell.
//
// The change-detection basis is content-hash sync: a live file appears in
// DeployResult.WrittenFiles only when its freshly rendered bytes differ from
// the bytes already on disk, so a written compose file is one whose rendered
// content (templates plus secrets) changed. A compose file is selected when:
//
//   - the file itself was written this run;
//   - a local file it reads (env_file, label_file, top-level configs/secrets
//     file:) was written or deleted this run;
//   - it reads an input bosun cannot map to a deployed path (include,
//     extends.file, an absolute or out-of-tree path, an interpolated path),
//     or an in-tree input bosun did not deploy this run (a file created on
//     the host, which content-hash sync never reports as written);
//   - one of its services sets a pull_policy that can change the image
//     without a config change (anything but missing, if_not_present, never);
//   - it declares a service with no present container (ServiceIsPresent:
//     running, restarting, or stopped by the restart breaker).
//
// Not detected, by design: a floating tag (for example :latest) updated
// outside bosun, and ${VAR} interpolation from bosun's own process
// environment. `bosun trigger -f` forces a full run that applies both.
// A service that never runs by design (inactive profile, scale 0, an exited
// one-shot job) is never present, so its file is selected on every deploy;
// that is no worse than the old run-everything behavior.
//
// Subset runs happen only when every entry in the compose directory is
// bosun-managed (in this run's deploy manifest); otherwise every file runs.
// Compose reads more from that directory than the files bosun passes with -f
// (a .env beside them, for one), and bosun cannot see changes to entries it
// does not deploy, so it fails closed on any of them.
//
// compose_ignore (#744) is the one exception: an operator-owned list of exact
// entry names (a separate project checked out by hand, say) that bosun leaves
// alone. An unmanaged entry on the list does not force a full run, is left
// out of the recorded unmanaged-entry list and its comparison, and is never
// read as a compose input: an ignored *.yml is not passed to compose up and
// is not inspected. An entry bosun deploys is never ignored, even when listed.
// `.env` cannot be listed, because compose reads it on its own. The orphan
// pass is unchanged: it acts on containers in bosun's compose project, not on
// directory entries, so containers an ignored file started under bosun's
// project name still count as orphans (and force a full run that removes
// them). An ignored file must run under its own project name.
//
// Every file also runs (today's behavior, orphan pass included) when any of
// these hold: the caller supplied a fallback reason (force, drift self-heal, a
// previous deploy left NeedsRedeploy set, no prior deploy or manifest,
// content-hash sync off, compose not synced this run); anything other than a
// top-level *.yml changed in the compose directory (.env, a fragment, a
// deletion); a compose file failed to parse; live container state is unknown;
// a container of any state belongs to no declared service (an orphan the
// --remove-orphans pass must clean); or a service declared at the last
// successful deploy is gone from every file.

// composeFileInputs is what one live compose file declares that change
// detection needs.
type composeFileInputs struct {
	// Path is the absolute live compose file path.
	Path string
	// Services are the service names the file declares.
	Services []string
	// Inputs are absolute paths of local files the file reads at compose time
	// (env_file, label_file, top-level configs/secrets file:).
	Inputs []string
	// Opaque is set when the file reads an input that cannot be mapped to a
	// deployed path; such a file is always selected. OpaqueReason says why.
	Opaque       bool
	OpaqueReason string
	// Err is set when the file could not be read or parsed.
	Err error
}

// inspectComposeFile reads one live compose file and records its services and
// the local files it reads. Relative input paths resolve against the compose
// file's directory (compose's project directory for a -f file). An input that
// resolves outside appdataRoot, or whose path uses interpolation, is opaque.
func inspectComposeFile(path, appdataRoot string) composeFileInputs {
	info := composeFileInputs{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		info.Err = fmt.Errorf("read compose file: %w", err)
		return info
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		info.Err = fmt.Errorf("parse compose file: %w", err)
		return info
	}

	baseDir := filepath.Dir(path)
	opaque := func(reason string) {
		if !info.Opaque {
			info.Opaque = true
			info.OpaqueReason = reason
		}
	}
	addInput := func(key string, raw string) {
		if raw == "" {
			opaque(key + " with an empty path")
			return
		}
		if strings.Contains(raw, "$") {
			opaque(fmt.Sprintf("%s path %q uses interpolation", key, raw))
			return
		}
		p := raw
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		p = filepath.Clean(p)
		if !withinAppdata(appdataRoot, p) {
			opaque(fmt.Sprintf("%s path %q is outside the deployed tree", key, raw))
			return
		}
		info.Inputs = append(info.Inputs, p)
	}
	// addPathList handles the string | [string | {path: string}] shape that
	// env_file and label_file share.
	addPathList := func(key string, v any) {
		switch t := v.(type) {
		case nil:
		case string:
			addInput(key, t)
		case []any:
			for _, item := range t {
				switch it := item.(type) {
				case string:
					addInput(key, it)
				case map[string]any:
					s, ok := it["path"].(string)
					if !ok {
						opaque(key + " entry without a string path")
						continue
					}
					addInput(key, s)
				default:
					opaque(key + " entry of an unknown shape")
				}
			}
		default:
			opaque(key + " of an unknown shape")
		}
	}

	if _, ok := doc["include"]; ok {
		// Included files carry their own services and inputs; bosun does not
		// follow them, so it cannot prove they are unchanged.
		opaque("uses include")
	}

	switch services := doc["services"].(type) {
	case nil:
	case map[string]any:
		for name, raw := range services {
			info.Services = append(info.Services, name)
			svc, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if pp, has := svc["pull_policy"]; has {
				switch pp {
				case "missing", "if_not_present", "never":
				default:
					opaque(fmt.Sprintf("service %s sets pull_policy %v, which can change the image without a config change", name, pp))
				}
			}
			if replicaCountAboveOne(svc) {
				// Presence is tracked per service, not per replica, so a lost
				// replica would look present; always re-apply instead.
				opaque(fmt.Sprintf("service %s runs more than one replica", name))
			}
			addPathList("env_file", svc["env_file"])
			addPathList("label_file", svc["label_file"])
			if ext, ok := svc["extends"].(map[string]any); ok {
				if _, hasFile := ext["file"]; hasFile {
					opaque("uses extends with a file")
				}
			}
		}
	default:
		info.Err = fmt.Errorf("parse compose file: services is not a mapping")
		return info
	}

	for _, section := range []string{"configs", "secrets"} {
		entries, ok := doc[section].(map[string]any)
		if !ok {
			if doc[section] != nil {
				opaque("top-level " + section + " of an unknown shape")
			}
			continue
		}
		for _, raw := range entries {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f, has := entry["file"]; has {
				s, ok := f.(string)
				if !ok {
					opaque(section + " file of an unknown shape")
					continue
				}
				addInput(section+" file", s)
			}
		}
	}

	sort.Strings(info.Services)
	sort.Strings(info.Inputs)
	return info
}

// composeSelectionInput is everything selectComposeFiles decides from. All
// paths are absolute live paths.
type composeSelectionInput struct {
	// Files are the live compose files, in run order.
	Files []composeFileInputs
	// ComposeDir is the live compose directory.
	ComposeDir string
	// Changed are the live paths written or deleted this run.
	Changed []string
	// FallbackReason, when non-empty, forces every file to run.
	FallbackReason string
	// ComposeDirEntries are the absolute paths of every entry directly in
	// the live compose directory; ComposeDirListed is false when the listing
	// was not taken or failed, which forces a full run.
	ComposeDirEntries []string
	ComposeDirListed  bool
	// PrevUnmanaged are the unmanaged compose-directory entry names recorded
	// at the last successful deploy; PrevUnmanagedKnown is false when no
	// record exists (older state, or the last deploy did not list).
	PrevUnmanaged      []string
	PrevUnmanagedKnown bool
	// Ignored are the compose_ignore entry names. An unmanaged entry whose
	// base name is listed is skipped by every compose-directory rule.
	Ignored map[string]bool
	// Managed are the live paths bosun deployed this run (the deploy
	// manifest). Content-hash sync reports changes only for these, so a
	// compose input outside it is untracked. Nil means none are tracked.
	Managed map[string]bool
	// LiveKnown reports whether Present and Containers reflect a successful
	// container listing taken before compose up.
	LiveKnown bool
	// Present are the service names with a present container (see
	// ServiceIsPresent).
	Present map[string]bool
	// Containers are the service names with a container in any state.
	Containers map[string]bool
	// PrevDeclared are the service names recorded at the last successful
	// deploy.
	PrevDeclared []string
}

// composeSelection is the result of selectComposeFiles.
type composeSelection struct {
	// Files are the compose files to run compose up on, in input order.
	Files []string
	// All is true when every file runs; only then does the orphan pass run.
	All bool
	// Reason explains a full run.
	Reason string
	// Reasons maps each selected file to why it was selected (subset runs).
	Reasons map[string]string
}

// selectComposeFiles decides which compose files a local deploy runs compose
// up on. See the comment at the top of this file for the rules.
func selectComposeFiles(in composeSelectionInput) composeSelection {
	all := func(reason string) composeSelection {
		files := make([]string, 0, len(in.Files))
		for _, f := range in.Files {
			files = append(files, f.Path)
		}
		return composeSelection{Files: files, All: true, Reason: reason}
	}

	if in.FallbackReason != "" {
		return all(in.FallbackReason)
	}
	if !in.LiveKnown {
		return all("live container state unknown")
	}
	if !in.ComposeDirListed {
		return all("compose directory could not be listed")
	}
	if !in.PrevUnmanagedKnown {
		return all("no record of unmanaged compose directory entries from the last deploy")
	}
	// Entries ignored now are dropped from the old record too, so adding a
	// name to compose_ignore takes effect on the next deploy.
	prevUnmanaged := withoutIgnored(in.PrevUnmanaged, in.Ignored)
	if cur := unmanagedComposeEntries(in.ComposeDirEntries, in.Managed, in.Ignored); len(prevUnmanaged) > 0 && !slices.Equal(prevUnmanaged, cur) {
		// An unmanaged entry present at the last deploy is gone or changed;
		// a host-deleted .env leaves no other trace.
		return all(fmt.Sprintf("unmanaged compose directory entries changed since the last deploy (was %s)", strings.Join(prevUnmanaged, ", ")))
	}
	for _, e := range in.ComposeDirEntries {
		if !managedEntry(filepath.Clean(e), in.Managed) && !in.Ignored[filepath.Base(e)] {
			return all(fmt.Sprintf("compose directory entry %q is not deployed by bosun", filepath.Base(e)))
		}
	}

	topLevel := make(map[string]bool, len(in.Files))
	for _, f := range in.Files {
		topLevel[filepath.Clean(f.Path)] = true
	}
	changed := make(map[string]bool, len(in.Changed))
	composeDir := filepath.Clean(in.ComposeDir)
	for _, c := range in.Changed {
		c = filepath.Clean(c)
		changed[c] = true
		if c == composeDir || withinAppdata(composeDir, c) {
			if !topLevel[c] {
				// A .env, a fragment, a subdirectory, or a deleted compose
				// file: it can change every file's effective config or
				// remove services.
				return all(fmt.Sprintf("compose directory entry %q changed", filepath.Base(c)))
			}
		}
	}

	declared := make(map[string]bool)
	for _, f := range in.Files {
		if f.Err != nil {
			return all(fmt.Sprintf("cannot inspect %s: %v", filepath.Base(f.Path), f.Err))
		}
		for _, s := range f.Services {
			declared[s] = true
		}
	}
	for _, s := range in.PrevDeclared {
		if !declared[s] {
			return all(fmt.Sprintf("service %q was removed", s))
		}
	}
	orphans := make([]string, 0)
	for s := range in.Containers {
		if !declared[s] {
			orphans = append(orphans, s)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return all(fmt.Sprintf("containers belong to no declared service: %s", strings.Join(orphans, ", ")))
	}

	sel := composeSelection{Files: []string{}, Reasons: map[string]string{}}
	for _, f := range in.Files {
		reason := composeFileSelectReason(f, changed, in.Managed, in.Present)
		if reason == "" {
			continue
		}
		sel.Files = append(sel.Files, f.Path)
		sel.Reasons[f.Path] = reason
	}
	if len(sel.Files) == len(in.Files) {
		// Every file runs anyway, so the set is complete and the orphan
		// pass is safe; run it as a full run would.
		sel.All = true
		sel.Reason = "every file selected"
	}
	return sel
}

// unmanagedComposeEntries returns the sorted base names of compose-directory
// entries that are not in the deploy manifest and not on the ignore list.
// Never nil.
func unmanagedComposeEntries(entries []string, managed, ignored map[string]bool) []string {
	out := []string{}
	for _, e := range entries {
		if !managedEntry(filepath.Clean(e), managed) && !ignored[filepath.Base(e)] {
			out = append(out, filepath.Base(e))
		}
	}
	sort.Strings(out)
	return out
}

// withoutIgnored returns names minus those on the ignore list, keeping order.
// Never nil.
func withoutIgnored(names []string, ignored map[string]bool) []string {
	out := []string{}
	for _, n := range names {
		if !ignored[n] {
			out = append(out, n)
		}
	}
	return out
}

// ValidateComposeIgnore checks compose_ignore entries. Each entry is the exact
// name of one entry directly in the compose directory: no globs, no path
// separators, not "." or "..", and not ".env", which compose reads by itself
// whether or not bosun passes it.
func ValidateComposeIgnore(names []string) error {
	for i, n := range names {
		switch {
		case n == "":
			return fmt.Errorf("compose_ignore[%d]: empty name", i)
		case n != strings.TrimSpace(n):
			return fmt.Errorf("compose_ignore[%d] %q: leading or trailing whitespace", i, n)
		case n == "." || n == "..":
			return fmt.Errorf("compose_ignore[%d] %q: must name an entry in the compose directory", i, n)
		case strings.ContainsAny(n, `/\`):
			return fmt.Errorf("compose_ignore[%d] %q: must be a bare entry name, without path separators", i, n)
		case strings.ContainsAny(n, "*?[]"):
			return fmt.Errorf("compose_ignore[%d] %q: must be an exact name; glob patterns are not supported", i, n)
		case strings.ContainsRune(n, 0):
			return fmt.Errorf("compose_ignore[%d] %q: contains a NUL byte", i, n)
		case n == ".env":
			return fmt.Errorf("compose_ignore[%d] %q: compose reads .env from the compose directory on its own, so it cannot be ignored", i, n)
		}
	}
	return nil
}

// ignoredComposeEntry reports whether a compose-directory path is skipped by
// compose_ignore: its base name is listed and bosun does not deploy it.
func ignoredComposeEntry(path string, managed, ignored map[string]bool) bool {
	return ignored[filepath.Base(path)] && !managedEntry(filepath.Clean(path), managed)
}

// filterIgnoredComposeFiles drops ignored compose files from the globbed
// list, so they are never inspected or passed to compose up.
func filterIgnoredComposeFiles(files []string, managed, ignored map[string]bool) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !ignoredComposeEntry(f, managed, ignored) {
			out = append(out, f)
		}
	}
	return out
}

// liveManagedSet returns the absolute live paths in this run's deploy manifest.
func liveManagedSet(appdata string, result *DeployResult) map[string]bool {
	managed := make(map[string]bool, len(result.ManagedFiles))
	for _, m := range result.ManagedFiles {
		managed[filepath.Join(appdata, filepath.FromSlash(m))] = true
	}
	return managed
}

// composeIgnoreSet returns the effective compose_ignore names as a set. An
// invalid list (config validation normally rejects it first; BOSUN_TARGETS
// can still carry one) is dropped whole, so selection fails closed. A name
// bosun deploys, in this run's manifest (managed, live paths) or the last
// deploy's (prevManaged, appdata-relative), is dropped with a warning: it
// stays managed even when this run does not sync compose/.
func (r *Reconciler) composeIgnoreSet(ctx context.Context, appdata, composeDir string, managed map[string]bool, prevManaged []string) map[string]bool {
	names := r.config.ComposeIgnore.Value
	if len(names) == 0 {
		return nil
	}
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	if err := ValidateComposeIgnore(names); err != nil {
		logger.Error().Err(err).
			Strs("compose_ignore", names).
			Msg("Encountered an invalid compose_ignore list, ignoring none of it. Reason: entries must be exact names in the compose directory")
		return nil
	}
	prev := make(map[string]bool, len(prevManaged))
	for _, m := range prevManaged {
		prev[filepath.Join(appdata, filepath.FromSlash(m))] = true
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		entry := filepath.Join(composeDir, n)
		if managedEntry(entry, managed) || managedEntry(entry, prev) {
			logger.Warn().
				Str("entry", n).
				Msg("Not ignoring compose directory entry listed in compose_ignore. Reason: bosun deploys it, so it stays managed")
			continue
		}
		set[n] = true
	}
	return set
}

// managedEntry reports whether a compose-directory entry is in the deploy
// manifest: the path itself, or (for a directory) any managed path beneath it.
func managedEntry(entry string, managed map[string]bool) bool {
	if managed[entry] {
		return true
	}
	prefix := entry + string(filepath.Separator)
	for m := range managed {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}

// replicaCountAboveOne reports whether a compose service declares more than
// one replica (scale or deploy.replicas). An interpolated or non-numeric
// value counts as more than one.
func replicaCountAboveOne(svc map[string]any) bool {
	above := func(v any) bool {
		switch n := v.(type) {
		case nil:
			return false
		case int:
			return n > 1
		default:
			return true
		}
	}
	if above(svc["scale"]) {
		return true
	}
	if dep, ok := svc["deploy"].(map[string]any); ok {
		return above(dep["replicas"])
	}
	return false
}

// composeFileSelectReason returns why one file must run, or "" to skip it.
func composeFileSelectReason(f composeFileInputs, changed, managed, present map[string]bool) string {
	if changed[filepath.Clean(f.Path)] {
		return "rendered content changed"
	}
	if f.Opaque {
		return "reads inputs bosun cannot track: " + f.OpaqueReason
	}
	for _, in := range f.Inputs {
		if !managed[in] {
			return "input " + in + " is not deployed by bosun, so its changes are not tracked"
		}
		for c := range changed {
			// Exact match for a file input; prefix match covers an input that
			// is a directory or a written parent directory.
			if c == in || withinAppdata(in, c) || withinAppdata(c, in) {
				return "input " + in + " changed"
			}
		}
	}
	for _, s := range f.Services {
		if !present[s] {
			return "service " + s + " has no present container"
		}
	}
	return ""
}

// composeFullRunReason returns why a deploy must run compose up on every file
// regardless of what changed, or "" when change detection may narrow the set.
func composeFullRunReason(force, forceRedeployUnchanged, priorNeedsRedeploy bool, lastDeployedCommit string, priorManagedCount int, contentHashSync bool) string {
	switch {
	case force:
		return "force"
	case forceRedeployUnchanged:
		return "drift self-heal"
	case priorNeedsRedeploy:
		return "previous deploy did not complete"
	case lastDeployedCommit == "":
		return "no previous successful deploy"
	case priorManagedCount == 0:
		return "no deploy manifest from the previous deploy"
	case !contentHashSync:
		return "content-hash sync is off, so written files are not a change signal"
	}
	return ""
}

// stagingRelToLive maps a staging-relative DeployResult path (as recorded
// after PrefixLatest, e.g. "appdata/traefik/x.yml" or "compose/a.yml") to its
// absolute live path. ok is false when no target owns the path.
func stagingRelToLive(targets []DeployTarget, appdata, rel string) (string, bool) {
	rel = filepath.Clean(rel)
	best := -1
	for i, t := range targets {
		tr := filepath.Clean(t.RelPath)
		if rel == tr || strings.HasPrefix(rel, tr+string(filepath.Separator)) {
			if best < 0 || len(tr) > len(filepath.Clean(targets[best].RelPath)) {
				best = i
			}
		}
	}
	if best < 0 {
		return "", false
	}
	t := targets[best]
	rest := strings.TrimPrefix(rel, filepath.Clean(t.RelPath))
	return filepath.Join(appdata, t.TargetPath, rest), true
}

// presentServices returns the service names with a container that
// ServiceIsPresent accepts (running, restarting, or stopped by the restart
// breaker), the same rule the pipeline's missing-service check uses (#725).
// A crash loop counts as present: compose up cannot fix it, so its unchanged
// file is not re-applied on every deploy.
func presentServices(actual []ActualService, restartTracking map[string]RestartTrackingEntry) map[string]bool {
	present := make(map[string]bool, len(actual))
	for _, svc := range actual {
		if ServiceIsPresent(svc.State, restartTracking[svc.Name].Tripped) {
			present[svc.Name] = true
		}
	}
	return present
}

// containerServices returns the service names with a container in any state.
func containerServices(actual []ActualService) map[string]bool {
	names := make(map[string]bool, len(actual))
	for _, svc := range actual {
		names[svc.Name] = true
	}
	return names
}

// selectLocalComposeFiles gathers the inputs for selectComposeFiles from this
// run's deploy result and the live compose files, decides, and logs the
// decision. composeSynced is false when compose/ was not a deploy target this
// run, in which case bosun did not write the files and cannot tell what
// changed.
//
// managed is liveManagedSet for this run and ignored is composeIgnoreSet;
// composeFiles has already had ignored files removed.
func (r *Reconciler) selectLocalComposeFiles(ctx context.Context, targets []DeployTarget, appdata, composeDir string, composeFiles []string, composeSynced bool, result *DeployResult, managed, ignored map[string]bool) composeSelection {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	fallback := r.composeFullRunReason
	if fallback == "" && !composeSynced {
		fallback = "compose directory was not synced this run"
	}

	var changed []string
	if fallback == "" {
		for _, rel := range append(append([]string{}, result.WrittenFiles...), result.DeletedFiles...) {
			live, ok := stagingRelToLive(targets, appdata, rel)
			if !ok {
				fallback = fmt.Sprintf("changed path %q maps to no deploy target", rel)
				break
			}
			changed = append(changed, live)
		}
	}

	// The listing is taken after the sync and before compose up; a host
	// write to compose/ inside that window is seen on the next deploy.
	var dirEntries []string
	dirListed := false
	if entries, err := os.ReadDir(composeDir); err != nil {
		logger.Warn().Err(err).Str(log.FieldPath, composeDir).Msg("Failed to list compose directory, running compose up on every file")
	} else {
		dirListed = true
		for _, e := range entries {
			dirEntries = append(dirEntries, filepath.Join(composeDir, e.Name()))
		}
	}
	logComposeIgnore(ctx, dirEntries, managed, ignored)

	files := make([]composeFileInputs, 0, len(composeFiles))
	for _, f := range composeFiles {
		files = append(files, inspectComposeFile(f, appdata))
	}
	prev := make([]string, 0, len(r.prevDeclaredServices))
	for _, s := range r.prevDeclaredServices {
		prev = append(prev, s.Name)
	}

	if dirListed {
		r.composeUnmanaged = unmanagedComposeEntries(dirEntries, managed, ignored)
		r.composeUnmanagedListed = true
	}

	sel := selectComposeFiles(composeSelectionInput{
		Files:              files,
		ComposeDir:         composeDir,
		Changed:            changed,
		FallbackReason:     fallback,
		ComposeDirEntries:  dirEntries,
		ComposeDirListed:   dirListed,
		PrevUnmanaged:      r.prevComposeUnmanaged,
		PrevUnmanagedKnown: r.prevComposeUnmanagedKnown,
		Ignored:            ignored,
		Managed:            managed,
		LiveKnown:          r.preDeployLiveKnown,
		Present:            r.preDeployPresent,
		Containers:         r.preDeployContainers,
		PrevDeclared:       prev,
	})

	if sel.All {
		logger.Info().
			Int("file_count", len(sel.Files)).
			Str("reason", sel.Reason).
			Msg("Running compose up on every compose file. Reason: change detection cannot narrow the set")
		ui.Info("  Compose up on all %d files (%s)", len(sel.Files), sel.Reason)
		return sel
	}
	for _, f := range sel.Files {
		logger.Info().
			Str(log.FieldPath, f).
			Str("reason", sel.Reasons[f]).
			Msg("Selected compose file for compose up")
	}
	logger.Info().
		Int("selected", len(sel.Files)).
		Int("skipped", len(composeFiles)-len(sel.Files)).
		Msg("Successfully narrowed compose up to changed files")
	ui.Info("  Compose up on %d of %d files (unchanged files skipped)", len(sel.Files), len(composeFiles))
	return sel
}

// logComposeIgnore logs, on every deploy with a compose_ignore list, which
// listed entries are present and skipped, so the list stays visible.
func logComposeIgnore(ctx context.Context, dirEntries []string, managed, ignored map[string]bool) {
	if len(ignored) == 0 {
		return
	}
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	configured := make([]string, 0, len(ignored))
	for n := range ignored {
		configured = append(configured, n)
	}
	sort.Strings(configured)
	skipped := []string{}
	for _, e := range dirEntries {
		name := filepath.Base(e)
		if !ignoredComposeEntry(e, managed, ignored) {
			continue
		}
		skipped = append(skipped, name)
	}
	sort.Strings(skipped)
	logger.Info().
		Strs("ignored", skipped).
		Strs("compose_ignore", configured).
		Msg("Skipping compose directory entries listed in compose_ignore. Reason: operator-owned, not deployed or read by bosun")
	if len(skipped) > 0 {
		ui.Info("  Ignoring compose directory entries: %s", strings.Join(skipped, ", "))
	}
}
