package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestAgentsLayout verifies that AGENTS.md lists exactly the packages
// under internal/, excluding testdata directories.
func TestAgentsLayout(t *testing.T) {
	// Get list of actual packages from go list
	actualPkgs := getInternalPackages(t)

	// Read AGENTS.md and extract backticked paths starting with internal/
	agentsPkgs := getAgentsPackages(t)

	// Check for missing packages (in actualPkgs but not in agentsPkgs)
	var missing []string
	for _, pkg := range actualPkgs {
		found := false
		for _, a := range agentsPkgs {
			if a == pkg {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, pkg)
		}
	}

	// Check for stale packages (in agentsPkgs but not in actualPkgs)
	var stale []string
	for _, a := range agentsPkgs {
		found := false
		for _, pkg := range actualPkgs {
			if pkg == a {
				found = true
				break
			}
		}
		if !found {
			stale = append(stale, a)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("packages missing from AGENTS.md: %v", missing)
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("packages in AGENTS.md but not found: %v", stale)
	}
}

// getInternalPackages returns a sorted list of internal packages.
func getInternalPackages(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "./internal/...")
	cmd.Dir = root(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var pkgs []string
	const prefix = "github.com/qunaxis/skenv/"

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip module prefix
		pkg := strings.TrimPrefix(line, prefix)
		// Skip testdata directories
		if !strings.Contains(pkg, "/testdata") {
			pkgs = append(pkgs, pkg)
		}
	}

	sort.Strings(pkgs)
	return pkgs
}

// getAgentsPackages extracts backticked paths starting with internal/
// from AGENTS.md. It extracts top-level packages only, stripping any
// files or subdirectories mentioned within them.
func getAgentsPackages(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(root(t), "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	// Match backticked paths like `internal/cli`, `internal/tools/gendocs`
	re := regexp.MustCompile("`(internal/[^`]+)`")
	matches := re.FindAllStringSubmatch(string(data), -1)

	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			path := m[1]
			// Extract package name: keep only the first component of a path
			// e.g., "internal/tools/gendocs" -> "internal/tools"
			//       "internal/cli/new.go" -> "internal/cli"
			//       "internal/cli" -> "internal/cli"
			pkg := extractPackageName(path)
			if pkg != "" {
				seen[pkg] = true
			}
		}
	}

	var pkgs []string
	for pkg := range seen {
		pkgs = append(pkgs, pkg)
	}

	sort.Strings(pkgs)
	return pkgs
}

// extractPackageName extracts the Go package path from a reference that may
// include files or subdirectories. Returns empty string if the reference is
// not a valid package path.
func extractPackageName(ref string) string {
	if !strings.HasPrefix(ref, "internal/") {
		return ""
	}

	// Split by /
	parts := strings.Split(ref, "/")
	if len(parts) < 2 {
		return ""
	}

	// If the first component after "internal/" is "tools", include it
	if parts[1] == "tools" && len(parts) >= 3 {
		// internal/tools/gendocs or internal/tools/genschemas
		return strings.Join(parts[:3], "/")
	}

	// For others, include only up to the first component
	return strings.Join(parts[:2], "/")
}
