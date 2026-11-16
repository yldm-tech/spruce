package advice_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/devjunk"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/engine/settings"
)

// categoryConsts reads the string constants named cat… that the scanner package in dir defines: its category IDs.
func categoryConsts(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "cat") || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						out[v] = true
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no category constants in %s", dir)
	}
	return out
}

// devjunkScan scans a temporary home with one tool cache and one project, as smartscan merges it.
func devjunkScan(t *testing.T) cleanup.ScanResult {
	t.Helper()
	home := t.TempDir()
	ws := filepath.Join(home, "Workspace")
	for _, f := range []string{"Library/Developer/Xcode/DerivedData/App/x", "Workspace/p/package.json", "Workspace/p/node_modules/x"} {
		p := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := devjunk.New(devjunk.Options{
		Home: home, Runner: execxtest.New(), GOOS: "darwin",
		Settings:      func() settings.CleanupSettings { return settings.CleanupSettings{DevProjectRoots: []string{ws}} },
		Getenv:        func(string) string { return "" },
		LookPath:      func(string) (string, error) { return "", os.ErrNotExist },
		VSCodeRunning: func(context.Context) bool { return false },
	})
	res, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Categories {
		res.Categories[i].ID = advice.SourceDevJunk + ":" + res.Categories[i].ID
		res.Categories[i].Source = advice.SourceDevJunk
	}
	return res
}

// TestTemplateTableNamesRealCategories: every category the template table names is one the scanners make, and every rule is one of the rules package.
func TestTemplateTableNamesRealCategories(t *testing.T) {
	system := categoryConsts(t, filepath.Join("..", "systemjunk"))
	installers := categoryConsts(t, filepath.Join("..", "installers"))
	tools := devjunk.ToolIDs()
	ids, prefixes, ruleIDs := advice.TemplateTable()
	for _, id := range ids {
		src, cat, _ := strings.Cut(id, ":")
		var ok bool
		switch src {
		case advice.SourceSystemJunk:
			ok = system[cat]
		case advice.SourceInstallers:
			ok = installers[cat]
		case advice.SourceDevJunk:
			tool, isTool := strings.CutPrefix(cat, "tool-")
			ok = isTool && slices.Contains(tools, tool)
		}
		if !ok {
			t.Errorf("the template table names %s, which no scanner makes", id)
		}
	}
	var scanned []string
	for _, c := range devjunkScan(t).Categories {
		scanned = append(scanned, c.ID)
	}
	for _, p := range prefixes {
		if !slices.ContainsFunc(scanned, func(id string) bool { return strings.HasPrefix(id, p) }) {
			t.Errorf("prefix %s matches no category of a devjunk scan (%v)", p, scanned)
		}
	}
	known := []string{string(rules.AppCaches), string(rules.Installers), string(rules.BuildOutput), string(rules.IncompleteDownloads)}
	for _, r := range ruleIDs {
		if !slices.Contains(known, r) {
			t.Errorf("rule %q is not a rules.ID", r)
		}
	}
	for _, r := range known {
		if !slices.Contains(ruleIDs, r) {
			t.Errorf("no card offers rule %s", r)
		}
	}
}

// TestDeriveOverAScan: the categories of a real devjunk scan make cards; these tiny ones all fold into 其他小项.
func TestDeriveOverAScan(t *testing.T) {
	res := devjunkScan(t)
	recs := advice.Derive(res, advice.Inputs{Now: time.Now()})
	if len(recs) != 1 || recs[0].ID != advice.IDSmall || !slices.Equal(recs[0].Categories, []string{"dev-junk:tool-xcode-derived-data", "dev-junk:artifact-node-modules"}) {
		t.Errorf("recs %+v", recs)
	}
}
