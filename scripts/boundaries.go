// Command boundaries checks architectural import boundaries for the project.
//
// It walks all Go source files and verifies that imports from the module's own
// packages follow the layer rules defined in docs/architecture.md. External
// dependencies and standard-library imports are always allowed.
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// rule describes which internal imports are allowed for a set of source paths.
type rule struct {
	name             string
	sourcePrefixes   []string
	allowedPrefixes  []string
	allowAllInternal bool
}

// knownExceptions permits a small number of existing imports that do not yet
// match the ideal layer model. Each entry is "sourcePrefix -> importPrefix".
var knownExceptions = []struct{ source, imp string }{
	// web uses the ActivityProvider contract that currently lives inside
	// internal/activity. Once that contract is extracted, remove this exception.
	{"web", "internal/activity"},
}

var rules = []rule{
	{
		name:             "cmd",
		sourcePrefixes:   []string{"cmd"},
		allowAllInternal: true,
	},
	{
		name:           "capture",
		sourcePrefixes: []string{"internal/activity"},
		allowedPrefixes: []string{
			"internal/clock",
			"internal/database",
		},
	},
	{
		name:           "domain",
		sourcePrefixes: []string{"internal/booking", "internal/closure", "internal/timeline"},
		allowedPrefixes: []string{
			"internal/booking",
			"internal/closure",
			"internal/timeline",
			"internal/clock",
			"internal/config",
			"internal/database",
			"internal/i18n",
		},
	},
	{
		name:            "infra",
		sourcePrefixes:  []string{"internal/clock", "internal/config", "internal/database", "internal/i18n", "internal/systemd", "internal/timeutil"},
		allowedPrefixes: []string{
			// Infrastructure packages may only depend on the standard library
			// and external dependencies. They must not import other internal
			// packages to avoid accidental coupling.
		},
	},
	{
		name:           "web",
		sourcePrefixes: []string{"web"},
		allowedPrefixes: []string{
			"internal/booking",
			"internal/closure",
			"internal/timeline",
			"internal/clock",
			"internal/config",
			"internal/database",
			"internal/i18n",
			"internal/timeutil",
		},
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	start, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	root, err := findModuleRoot(start)
	if err != nil {
		return err
	}

	module, err := modulePath(root)
	if err != nil {
		return err
	}

	violations, filesChecked, uncovered, err := check(root, module)
	if err != nil {
		return err
	}

	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		fmt.Fprintln(os.Stderr, "error: the following source paths are not covered by a boundary rule:")
		for _, p := range uncovered {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
		return fmt.Errorf("uncovered source paths")
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		for _, v := range violations {
			fmt.Fprintln(os.Stderr, v)
		}
		return fmt.Errorf("boundary lint failed")
	}

	fmt.Printf("Boundary lint passed for %d files.\n", filesChecked)
	return nil
}

func check(root, module string) (violations []string, filesChecked int, uncovered []string, err error) {
	modulePrefix := module + "/"

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(rel, ".go") {
			return nil
		}

		filesChecked++

		layer := matchingLayer(rel)
		if layer == nil {
			if isInternalSource(rel) {
				uncovered = append(uncovered, rel)
			}
			return nil
		}

		imports, parseErr := fileImports(path)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", rel, parseErr)
		}

		for _, imp := range imports {
			if !strings.HasPrefix(imp, modulePrefix) && imp != module {
				continue
			}

			internal := strings.TrimPrefix(imp, modulePrefix)
			if internal == "" {
				continue
			}

			if layer.allowAllInternal {
				continue
			}

			if isExcepted(rel, internal) {
				continue
			}

			allowed := false
			for _, prefix := range layer.allowedPrefixes {
				if matchesPrefix(internal, prefix) {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, fmt.Sprintf(
					"%s file %s imports forbidden package %s",
					layer.name, rel, imp,
				))
			}
		}

		return nil
	})

	return violations, filesChecked, uncovered, err
}

func shouldSkipDir(rel string) bool {
	if rel == "." {
		return false
	}
	// scripts holds standalone tooling (this checker included) that is not part
	// of the layered application and therefore has no boundary rules to enforce.
	if strings.HasPrefix(rel, ".") || rel == "vendor" || rel == "scripts" {
		return true
	}
	return false
}

// findModuleRoot walks up from start until it finds a directory containing
// go.mod, so the check works regardless of the directory it is invoked from.
func findModuleRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in %s or any parent directory", start)
		}
		dir = parent
	}
}

func isInternalSource(rel string) bool {
	return strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "web/")
}

func matchingLayer(rel string) *rule {
	var best *rule
	bestLen := -1
	for i := range rules {
		r := &rules[i]
		for _, prefix := range r.sourcePrefixes {
			if matchesPrefix(rel, prefix) {
				if len(prefix) > bestLen {
					best = r
					bestLen = len(prefix)
				}
			}
		}
	}
	return best
}

func isExcepted(source, internal string) bool {
	for _, exc := range knownExceptions {
		if matchesPrefix(source, exc.source) && matchesPrefix(internal, exc.imp) {
			return true
		}
	}
	return false
}

func matchesPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func fileImports(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imports := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("unquote import %s: %w", spec.Path.Value, err)
		}
		imports = append(imports, imp)
	}
	return imports, nil
}

var moduleRegexp = regexp.MustCompile(`(?m)^module\s+(\S+)`)

func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	m := moduleRegexp.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("could not find module path in go.mod")
	}
	return string(m[1]), nil
}
