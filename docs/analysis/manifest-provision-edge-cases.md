# Manifest/Provision Workflow Edge Case Analysis

This document identifies missing error handling, edge cases, validation gaps, and output issues in the bosun CLI's manifest and provision workflows.

## Summary

| Category | Severity | Count |
|----------|----------|-------|
| Critical | High | 3 |
| Data Validation | Medium | 7 |
| Edge Cases | Medium | 6 |
| Output Issues | Low | 3 |
| Error Messages | Low | 4 |

---

## 1. Missing Error Handling

### 1.1 YAML Parsing Errors (Medium)

**Location:** `render.go:129`, `render.go:143`, `render.go:238`, `render.go:253`

**Issue:** YAML parsing errors are wrapped but do not provide line/column information.

**Current behavior:**
```go
if err := yaml.Unmarshal(content, &manifest); err != nil {
    return nil, fmt.Errorf("parse manifest: %w", err)
}
```

**Problem:** When YAML is malformed, users get cryptic messages like:
```
parse manifest: yaml: line 15: mapping values are not allowed in this context
```

**Recommendation:** Add helper function to extract and format YAML error location with context (show surrounding lines).

---

### 1.2 Missing Variable Errors - Good (No Issue)

**Location:** `interpolate.go:31-33`

**Current behavior:** Already provides helpful error messages listing all missing variables:
```go
return "", fmt.Errorf("missing variables: ${%s}", strings.Join(missingVars, "}, ${"))
```

**Output:** `missing variables: ${domain}, ${port}`

This is already well-handled.

---

### 1.3 File Not Found - Good (No Issue)

**Location:** `provision.go:28-29`

**Current behavior:** Clear message with full path:
```go
return nil, fmt.Errorf("provision not found: %s", provisionPath)
```

This is already well-handled.

---

### 1.4 Empty Manifest Name (Critical)

**Location:** `types.go:6-28`, `render.go:12-106`

**Issue:** Service manifests with empty `name` field are accepted without validation.

**Impact:**
- Variable `${name}` interpolates to empty string
- Output files may have invalid names
- Compose service names become empty

**Recommendation:** Add validation in `LoadServiceManifest()`:
```go
if manifest.Name == "" {
    return nil, fmt.Errorf("manifest missing required 'name' field")
}
```

---

## 2. Edge Cases Not Covered

### 2.1 Empty Provisions List (Low)

**Location:** `render.go:30-37`

**Issue:** A manifest with `provisions: []` silently produces empty output.

**Current behavior:** Loop simply doesn't execute, returns empty `RenderOutput`.

**Recommendation:** Consider warning when a non-raw manifest has no provisions:
```go
if len(manifest.Provisions) == 0 && manifest.Type != "raw" && len(manifest.Needs) == 0 {
    // Log warning or return error
}
```

---

### 2.2 Circular Includes - Partially Handled (Medium)

**Location:** `provision.go:19-23`

**Current behavior:**
```go
if loaded[provisionName] {
    return &Provision{}, nil
}
```

**Issue:** Circular includes are silently ignored by returning an empty provision. No warning or error is logged.

**Impact:** User creates `a.yml` including `b.yml` which includes `a.yml`. The circular reference silently produces incomplete output.

**Recommendation:**
1. Return an error or warning when circular include detected
2. Track the include chain for better error messages:
```go
if loaded[provisionName] {
    return nil, fmt.Errorf("circular include detected: %s already in include chain", provisionName)
}
```

---

### 2.3 Very Deep Nesting in YAML (Low)

**Location:** `merge.go:29-79`, `interpolate.go:73-92`

**Issue:** No recursion depth limit. Extremely deep YAML structures could cause stack overflow.

**Current behavior:** Recursive functions `deepMergeInternal`, `interpolateValue`, `deepCopy` have no depth limit.

**Impact:** Pathological input with 1000+ levels of nesting could crash the process.

**Recommendation:** Add depth counter parameter with reasonable limit (e.g., 100):
```go
func deepMergeInternal(base, overlay map[string]any, path string, depth int) map[string]any {
    if depth > 100 {
        panic("YAML nesting too deep (max 100 levels)")
    }
    // ...
}
```

---

### 2.4 Unicode in Service Names (Medium)

**Location:** `types.go:8`, `render.go:20`

**Issue:** Service names are not validated for Docker/compose compatibility.

**Impact:** Names like `サービス` or `my service` or `my:service` will pass through and cause Docker compose errors later.

**Current behavior:** Any string accepted.

**Recommendation:** Validate service names against Docker naming rules:
```go
var validServiceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validateServiceName(name string) error {
    if !validServiceName.MatchString(name) {
        return fmt.Errorf("invalid service name %q: must match [a-zA-Z0-9][a-zA-Z0-9_.-]*", name)
    }
    return nil
}
```

---

### 2.5 Special Characters in Config Values (Low)

**Location:** `interpolate.go:15-36`

**Issue:** Interpolated values containing special YAML characters could break parsing.

**Example:**
```yaml
config:
  password: "my:password"
```

When interpolated into:
```yaml
db_url: postgres://user:${password}@host
```

Results in:
```yaml
db_url: postgres://user:my:password@host  # Invalid YAML if not quoted
```

**Current behavior:** String is interpolated as-is, before YAML parsing.

**Mitigation:** This is actually mitigated because interpolation happens BEFORE YAML parsing (`provision.go:35`). However, values containing `${` could cause issues.

**Recommendation:** Escape or validate special sequences in interpolated values.

---

### 2.6 Missing Required Fields in Manifest (Critical)

**Location:** `types.go:6-28`

**Issue:** No validation of required fields. All fields are optional.

**Missing validations:**
- `name` - required but not validated
- `provisions` - at least one required (unless raw mode or needs)
- When `type: raw`, `compose` field should be required

**Recommendation:** Add `Validate()` method to `ServiceManifest`:
```go
func (m *ServiceManifest) Validate() error {
    if m.Name == "" {
        return fmt.Errorf("missing required field: name")
    }
    if m.Type != "raw" && len(m.Provisions) == 0 && len(m.Needs) == 0 && len(m.Services) == 0 {
        return fmt.Errorf("manifest must have provisions, needs, or services (or type: raw)")
    }
    if m.Type == "raw" && m.Compose == nil {
        return fmt.Errorf("raw type requires compose field")
    }
    return nil
}
```

---

### 2.7 Invalid Port Numbers (Medium)

**Location:** N/A (no validation)

**Issue:** Port numbers in config are not validated.

**Impact:** Invalid ports like `-1`, `70000`, or `abc` pass through to compose output.

**Current behavior:** Any value accepted in config.

**Recommendation:** Add port validation helper used when known port fields are accessed:
```go
func validatePort(value any) error {
    port, ok := value.(int)
    if !ok {
        return fmt.Errorf("port must be integer, got %T", value)
    }
    if port < 1 || port > 65535 {
        return fmt.Errorf("port must be 1-65535, got %d", port)
    }
    return nil
}
```

---

### 2.8 Conflicting Network Definitions (Low)

**Location:** `render.go:165-168`

**Issue:** Stack networks override rather than merge with service-defined networks.

**Current behavior:**
```go
if stack.Networks != nil {
    output.Compose["networks"] = stack.Networks
}
```

**Impact:** If a service defines a network and stack also defines networks, service networks are lost.

**Recommendation:** Use `DeepMerge` for networks:
```go
if stack.Networks != nil {
    if existing, ok := output.Compose["networks"].(map[string]any); ok {
        output.Compose["networks"] = DeepMerge(existing, stack.Networks)
    } else {
        output.Compose["networks"] = stack.Networks
    }
}
```

---

## 3. Data Validation Gaps

### 3.1 Service Name Validation (Medium)

**Status:** Not implemented

**Required validation:**
- Must not be empty
- Must match Docker naming: `[a-zA-Z0-9][a-zA-Z0-9_.-]*`
- Reasonable length limit (63 chars for DNS compatibility)

---

### 3.2 Port Number Validation (Medium)

**Status:** Not implemented

**Required validation:**
- Integer type
- Range 1-65535
- For exposed ports, could check for privileged ports (< 1024)

---

### 3.3 Image Name Validation (Low)

**Status:** Not implemented

**Required validation:**
- Valid Docker image reference format
- Tag or digest present (optional but recommended)

---

### 3.4 Domain Name Format Validation (Medium)

**Status:** Not implemented

**Required validation:**
- Valid DNS hostname format
- No wildcard in wrong positions
- Reasonable length

---

## 4. Merge Semantics Edge Cases

### 4.1 Type Mismatch on Same Key (Critical)

**Location:** `merge.go:44-76`

**Issue:** When base has map and overlay has scalar (or vice versa), overlay silently wins.

**Example:**
```yaml
# base provision
compose:
  environment:
    FOO: bar

# overlay provision
compose:
  environment: ["FOO=baz"]  # List vs map!
```

**Current behavior:** List replaces map entirely (line 75: default case).

**Impact:** Unexpected behavior, no warning.

**Recommendation:** Log warning or error when types mismatch:
```go
baseMap, baseIsMap := baseValue.(map[string]any)
overlayMap, overlayIsMap := overlayValue.(map[string]any)

if (baseIsMap && !overlayIsMap) || (!baseIsMap && overlayIsMap) {
    log.Printf("Warning: type mismatch at %s: base is %T, overlay is %T", currentPath, baseValue, overlayValue)
}
```

---

### 4.2 List Duplicates After Union (Low)

**Location:** `merge.go:137-155`

**Current behavior:** `stringSliceUnion` correctly handles duplicates within the union operation.

**No issue found** - implementation is correct.

---

### 4.3 Environment Variable Override Priority (Low)

**Location:** `merge.go:49-54`

**Current behavior:** When both are maps, `normalizeToDict` is called and then `DeepMerge` - overlay values win.

**This is correct behavior** - overlay values should override base values.

---

## 5. Output Issues

### 5.1 YAML Output Key Order (Low)

**Location:** `render.go:199`, `render.go:222`

**Issue:** `yaml.Marshal` in Go produces non-deterministic key order.

**Impact:**
- Git diffs show false changes when keys are reordered
- Makes output harder to review

**Current behavior:** Keys in random order each run.

**Recommendation:** Use custom YAML encoder with sorted keys, or pre-sort maps before marshaling.

---

### 5.2 File Permissions on Output (Low)

**Location:** `render.go:204`

**Current behavior:** Files written with `0644` permissions.

**This is correct** - readable by all, writable by owner only.

---

### 5.3 Read-Only Output Directory (Medium)

**Location:** `render.go:175`, `render.go:194`

**Issue:** Error messages for read-only directories are unclear.

**Current behavior:**
```go
if err := os.MkdirAll(outputDir, 0755); err != nil {
    return fmt.Errorf("create output directory: %w", err)
}
```

**Output:** `create output directory: mkdir /readonly/path: permission denied`

**Recommendation:** Pre-check write permissions with clearer message:
```go
if err := checkWritable(outputDir); err != nil {
    return fmt.Errorf("output directory not writable: %s", outputDir)
}
```

---

## 6. Error Message Improvements

### 6.1 YAML Parse Errors Need Context

**Current:** `parse provision webapp: yaml: line 15: did not find expected key`

**Improved:** Include file path, show surrounding lines:
```
parse provision webapp (/path/to/provisions/webapp.yml):
  line 15: did not find expected key

  13 |   environment:
  14 |     FOO: bar
> 15 |     BAZ
  16 |   networks:
```

---

### 6.2 Include Chain in Errors

**Current:** `include redis in webapp: provision not found: /path/redis.yml`

**Improved:** Show full chain:
```
provision not found: /path/redis.yml
  include chain: core -> webapp -> redis
```

---

### 6.3 Validation Errors Should Be Aggregated

**Current:** First error returned, others missed.

**Improved:** Collect all validation errors and return together:
```
manifest validation failed:
  - missing required field: name
  - invalid port 70000: must be 1-65535
  - service name "my service" invalid: contains space
```

---

### 6.4 Dry Run Should Show Warnings

**Location:** `cmd/provision.go:132-139`

**Issue:** Dry run only shows YAML, doesn't surface warnings about potential issues.

**Recommendation:** Add validation warnings even in dry run mode.

---

## 7. Recommendations Summary

### High Priority (Should Fix)

1. **Add manifest validation** - Validate required fields, service names, ports
2. **Fix circular include handling** - Error or warn instead of silent empty return
3. **Warn on type mismatch** - Log when base/overlay types conflict during merge

### Medium Priority (Should Consider)

4. **Network merge in stacks** - Use DeepMerge instead of replace
5. **Deterministic YAML output** - Sort keys for stable diffs
6. **Improve YAML error messages** - Add line context

### Low Priority (Nice to Have)

7. **Recursion depth limit** - Prevent stack overflow on pathological input
8. **Empty provisions warning** - Alert when manifest produces no output
9. **Pre-check output directory** - Better permission error messages

---

## 8. Test Cases to Add

Based on this analysis, the following test cases should be added:

```go
// Edge case tests needed
func TestEmptyManifestName(t *testing.T) {}
func TestCircularIncludes(t *testing.T) {}
func TestTypeMismatchMerge(t *testing.T) {}
func TestUnicodeServiceName(t *testing.T) {}
func TestInvalidPortNumber(t *testing.T) {}
func TestDeepNesting(t *testing.T) {}
func TestEmptyProvisionsList(t *testing.T) {}
func TestStackNetworkMerge(t *testing.T) {}
func TestSpecialCharsInInterpolation(t *testing.T) {}
func TestYAMLOutputDeterminism(t *testing.T) {}
```
