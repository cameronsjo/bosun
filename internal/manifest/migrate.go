package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// MigrationResult represents the result of migrating a single file.
type MigrationResult struct {
	// Path is the file path that was processed.
	Path string

	// Kind is the detected or inferred manifest kind.
	Kind string

	// WasVersioned indicates if the file already had apiVersion/kind.
	WasVersioned bool

	// Migrated indicates if the file was migrated (or would be in dry-run).
	Migrated bool

	// Error contains any error that occurred during migration.
	Error error
}

// MigrateOptions configures the migration behavior.
type MigrateOptions struct {
	// DryRun if true, don't write changes to disk.
	DryRun bool

	// Verbose if true, include more detail in results.
	Verbose bool
}

// MigrateToV1 adds apiVersion and kind fields to an unversioned manifest.
// It detects the manifest type based on content and adds appropriate headers.
// Returns the migrated content and the kind.
//
// The result is one YAML document that carries both fields, or an error and no
// content. A leading document marker stays first with the header after it, a
// half-versioned file gets only the missing field (an existing non-empty value
// is kept, an empty one is replaced in place), and a file that holds several
// documents is rejected. The result is parsed again and compared with the input
// before it is returned, so a caller that writes it never writes a file that no
// longer parses or whose body changed.
func MigrateToV1(data []byte) ([]byte, string, error) {
	// Check if already versioned
	versioned, err := IsVersioned(data)
	if err != nil {
		return nil, "", fmt.Errorf("check versioned: %w", err)
	}

	if versioned {
		// Already versioned, return as-is
		kind, err := GetManifestKind(data)
		if err != nil {
			return nil, "", err
		}
		return data, kind, nil
	}

	if err := requireSingleDocument(data); err != nil {
		return nil, "", err
	}

	// Detect kind from content
	detected, err := detectKind(data)
	if err != nil {
		return nil, "", fmt.Errorf("detect kind: %w", err)
	}

	migrated, kind, err := addVersionHeader(data, detected)
	if err != nil {
		return nil, "", err
	}
	if err := verifyMigration(data, migrated); err != nil {
		return nil, "", err
	}

	return migrated, kind, nil
}

// requireSingleDocument rejects data that holds more than one YAML document. An
// empty file holds none and is accepted.
func requireSingleDocument(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	documents := 0
	for {
		var doc yaml.Node
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
		documents++
		if documents > 1 {
			return errors.New("file holds more than one YAML document; split it before migrating")
		}
	}
}

// isEmptyYAMLValue reports whether a scalar is null or an empty string, which
// IsVersioned also treats as absent.
func isEmptyYAMLValue(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && (node.Tag == "!!null" || node.Value == "")
}

// leadingMarkerIndex returns the index of a leading "---" line, skipping blank
// and comment lines before it, or -1 when the document has none. A marker line
// that carries anything after the dashes is an error: the header cannot be
// placed after it safely.
func leadingMarkerIndex(lines []string) (int, error) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "---" {
			return i, nil
		}
		if strings.HasPrefix(trimmed, "---") {
			return -1, errors.New("document marker line carries content; put the content on the next line before migrating")
		}
		return -1, nil
	}
	return -1, nil
}

// addVersionHeader returns data with apiVersion and kind present and non-empty,
// and the kind it carries. detected is the kind inferred from the content, used
// when the file does not name one.
func addVersionHeader(data []byte, detected string) ([]byte, string, error) {
	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("parse manifest: %w", err)
	}

	// Locate the two header keys in the top-level mapping.
	var apiKey, apiValue, kindKey, kindValue *yaml.Node
	if len(doc.Content) > 0 {
		root := doc.Content[0]
		if root.Kind == yaml.MappingNode {
			if root.Style&yaml.FlowStyle != 0 {
				return nil, "", errors.New("top-level mapping is in flow style; rewrite it in block style before migrating")
			}
			for i := 0; i+1 < len(root.Content); i += 2 {
				switch root.Content[i].Value {
				case "apiVersion":
					apiKey, apiValue = root.Content[i], root.Content[i+1]
				case "kind":
					kindKey, kindValue = root.Content[i], root.Content[i+1]
				}
			}
		}
	}

	// An existing non-empty apiVersion stays as written, so only the kind needs
	// resolving here.
	kind := detected
	if kindValue != nil && !isEmptyYAMLValue(kindValue) {
		kind = kindValue.Value
	}

	// replaceLine rewrites the single line a key and its empty value sit on.
	replaceLine := func(key, value *yaml.Node, text string) error {
		if value.Line != key.Line || key.Line < 1 || key.Line > len(lines) {
			return fmt.Errorf("cannot rewrite %q in place: its value is not on the same line", key.Value)
		}
		line := lines[key.Line-1]
		eol := ""
		switch {
		case strings.HasSuffix(line, "\r\n"):
			eol = "\r\n"
		case strings.HasSuffix(line, "\n"):
			eol = "\n"
		}
		lines[key.Line-1] = text + eol
		return nil
	}

	apiEmpty := apiValue != nil && isEmptyYAMLValue(apiValue)
	kindEmpty := kindValue != nil && isEmptyYAMLValue(kindValue)
	apiLine := fmt.Sprintf("apiVersion: %s", APIVersionV1)
	kindLine := fmt.Sprintf("kind: %s", kind)

	if apiEmpty {
		if err := replaceLine(apiKey, apiValue, apiLine); err != nil {
			return nil, "", err
		}
	}
	if kindEmpty {
		if err := replaceLine(kindKey, kindValue, kindLine); err != nil {
			return nil, "", err
		}
	}

	insert := func(at int, text ...string) {
		out := make([]string, 0, len(lines)+len(text))
		out = append(out, lines[:at]...)
		for _, t := range text {
			out = append(out, t+newline)
		}
		out = append(out, lines[at:]...)
		lines = out
	}

	switch {
	case apiValue == nil && kindValue == nil:
		// Neither key: the header goes first, or right after a leading marker.
		at := 0
		marker, err := leadingMarkerIndex(lines)
		if err != nil {
			return nil, "", err
		}
		if marker >= 0 {
			at = marker + 1
		}
		insert(at, apiLine, kindLine)
	case apiValue == nil:
		// Only kind: apiVersion goes just before it.
		insert(kindKey.Line-1, apiLine)
	case kindValue == nil:
		// Only apiVersion: kind goes just after it, which needs the line to end.
		at := apiKey.Line
		if at > len(lines) {
			return nil, "", errors.New("cannot find the apiVersion line")
		}
		if !strings.HasSuffix(lines[at-1], "\n") {
			lines[at-1] += newline
		}
		insert(at, kindLine)
	}

	return []byte(strings.Join(lines, "")), kind, nil
}

// verifyMigration checks the migrated content before it is returned: one
// document, versioned, and the same fields as the input apart from the two
// header keys.
func verifyMigration(original, migrated []byte) error {
	if err := requireSingleDocument(migrated); err != nil {
		return fmt.Errorf("migrated content is not one document: %w", err)
	}
	versioned, err := IsVersioned(migrated)
	if err != nil {
		return fmt.Errorf("migrated content does not parse: %w", err)
	}
	if !versioned {
		return errors.New("migrated content does not carry both apiVersion and kind")
	}

	before, err := bodyWithoutHeader(original)
	if err != nil {
		return err
	}
	after, err := bodyWithoutHeader(migrated)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("migrated content differs from the input beyond the header")
	}
	return nil
}

// bodyWithoutHeader decodes a manifest and drops apiVersion and kind.
func bodyWithoutHeader(data []byte) (map[string]any, error) {
	body := map[string]any{}
	if err := yaml.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if body == nil {
		body = map[string]any{}
	}
	delete(body, "apiVersion")
	delete(body, "kind")
	return body, nil
}

// detectKind infers the manifest kind from its content.
func detectKind(data []byte) (string, error) {
	var content map[string]any
	if err := yaml.Unmarshal(data, &content); err != nil {
		return "", fmt.Errorf("parse content: %w", err)
	}

	// Stack: has 'include' key (list of service files)
	if _, hasInclude := content["include"]; hasInclude {
		return KindStack, nil
	}

	// Service: has 'name' + ('provisions' or 'needs' or 'services' or 'type')
	if _, hasName := content["name"]; hasName {
		if _, hasProvisions := content["provisions"]; hasProvisions {
			return KindService, nil
		}
		if _, hasNeeds := content["needs"]; hasNeeds {
			return KindService, nil
		}
		if _, hasServices := content["services"]; hasServices {
			return KindService, nil
		}
		if _, hasType := content["type"]; hasType {
			return KindService, nil
		}
	}

	// Provision: has 'compose', 'traefik', 'gatus', or 'includes' (provision inheritance)
	if _, hasCompose := content["compose"]; hasCompose {
		return KindProvision, nil
	}
	if _, hasTraefik := content["traefik"]; hasTraefik {
		return KindProvision, nil
	}
	if _, hasGatus := content["gatus"]; hasGatus {
		return KindProvision, nil
	}
	if _, hasIncludes := content["includes"]; hasIncludes {
		return KindProvision, nil
	}

	// Default to Provision for empty or unrecognized content
	return KindProvision, nil
}

// MigrateFile migrates a single manifest file to v1.
func MigrateFile(path string, opts MigrateOptions) (*MigrationResult, error) {
	result := &MigrationResult{Path: path}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		result.Error = fmt.Errorf("read file: %w", err)
		return result, result.Error
	}

	// Check if already versioned
	versioned, err := IsVersioned(data)
	if err != nil {
		result.Error = fmt.Errorf("check versioned: %w", err)
		return result, result.Error
	}

	result.WasVersioned = versioned
	if versioned {
		kind, _ := GetManifestKind(data)
		result.Kind = kind
		result.Migrated = false
		return result, nil
	}

	// Migrate
	migrated, kind, err := MigrateToV1(data)
	if err != nil {
		result.Error = fmt.Errorf("migrate: %w", err)
		return result, result.Error
	}

	result.Kind = kind
	result.Migrated = true

	// Write if not dry-run
	if !opts.DryRun {
		if err := os.WriteFile(path, migrated, 0644); err != nil {
			result.Error = fmt.Errorf("write file: %w", err)
			return result, result.Error
		}
	}

	return result, nil
}

// MigrateDirectory migrates all manifest files in a directory.
// Looks for .yml and .yaml files in the specified directories.
func MigrateDirectory(dirs []string, opts MigrateOptions) ([]*MigrationResult, error) {
	var results []*MigrationResult

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip non-existent directories
			}
			return results, fmt.Errorf("read directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			ext := filepath.Ext(name)
			if ext != ".yml" && ext != ".yaml" {
				continue
			}

			path := filepath.Join(dir, name)
			result, _ := MigrateFile(path, opts)
			results = append(results, result)
		}
	}

	return results, nil
}

// ScanUnversioned finds all unversioned manifest files in the given directories.
func ScanUnversioned(dirs []string) ([]*MigrationResult, error) {
	var results []*MigrationResult

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return results, fmt.Errorf("read directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			ext := filepath.Ext(name)
			if ext != ".yml" && ext != ".yaml" {
				continue
			}

			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				results = append(results, &MigrationResult{
					Path:  path,
					Error: err,
				})
				continue
			}

			versioned, err := IsVersioned(data)
			if err != nil {
				results = append(results, &MigrationResult{
					Path:  path,
					Error: err,
				})
				continue
			}

			if !versioned {
				kind, _ := detectKind(data)
				results = append(results, &MigrationResult{
					Path:         path,
					Kind:         kind,
					WasVersioned: false,
					Migrated:     false,
				})
			}
		}
	}

	return results, nil
}

// FormatMigrationSummary creates a human-readable summary of migration results.
func FormatMigrationSummary(results []*MigrationResult, dryRun bool) string {
	var sb strings.Builder

	var migrated, skipped, errors int
	for _, r := range results {
		if r.Error != nil {
			errors++
		} else if r.Migrated {
			migrated++
		} else {
			skipped++
		}
	}

	action := "Migrated"
	if dryRun {
		action = "Would migrate"
	}

	_, _ = fmt.Fprintf(&sb, "\n%s: %d files\n", action, migrated)
	_, _ = fmt.Fprintf(&sb, "Already versioned: %d files\n", skipped)
	if errors > 0 {
		_, _ = fmt.Fprintf(&sb, "Errors: %d files\n", errors)
	}

	if migrated > 0 {
		sb.WriteString("\nFiles requiring migration:\n")
		for _, r := range results {
			if r.Migrated {
				_, _ = fmt.Fprintf(&sb, "  - %s (detected: %s)\n", r.Path, r.Kind)
			}
		}
	}

	if errors > 0 {
		sb.WriteString("\nFiles with errors:\n")
		for _, r := range results {
			if r.Error != nil {
				_, _ = fmt.Fprintf(&sb, "  - %s: %v\n", r.Path, r.Error)
			}
		}
	}

	return sb.String()
}
