package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const allowlistPath = "tools/jsonwrapcheck/allowlist.txt"

var forbiddenJSONCalls = map[string]struct{}{
	"Compact":       {},
	"HTMLEscape":    {},
	"Indent":        {},
	"Marshal":       {},
	"MarshalIndent": {},
	"NewDecoder":    {},
	"NewEncoder":    {},
	"Unmarshal":     {},
	"Valid":         {},
}

type finding struct {
	Path     string
	Line     int
	Function string
	Selector string
	ExprHash string
	Expr     string
}

func main() {
	listMode := flag.Bool("list", false, "print the current allowlist entries")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	findings, err := scan(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "json wrapper check failed: %v\n", err)
		os.Exit(1)
	}
	sortFindings(findings)

	if *listMode {
		printAllowlist(findings)
		return
	}

	allowlist, err := loadAllowlist(filepath.Join(root, allowlistPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "json wrapper check failed: %v\n", err)
		os.Exit(1)
	}

	unused := make(map[string]int, len(allowlist))
	for key, count := range allowlist {
		unused[key] = count
	}

	var unexpected []finding
	for _, item := range findings {
		key := item.key()
		if unused[key] > 0 {
			unused[key]--
			continue
		}
		unexpected = append(unexpected, item)
	}

	var stale []string
	for key, count := range unused {
		for i := 0; i < count; i++ {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(unexpected) > 0 || len(stale) > 0 {
		if len(unexpected) > 0 {
			fmt.Fprintln(os.Stderr, "direct encoding/json calls not in the allowlist:")
			for _, item := range unexpected {
				fmt.Fprintf(os.Stderr, "  %s:%d %s %s\n", item.Path, item.Line, item.Selector, item.Expr)
			}
		}
		if len(stale) > 0 {
			fmt.Fprintln(os.Stderr, "stale json wrapper allowlist entries:")
			for _, key := range stale {
				fmt.Fprintf(os.Stderr, "  %s\n", key)
			}
		}
		fmt.Fprintf(os.Stderr, "Regenerate the baseline with: go run ./tools/jsonwrapcheck -list > %s\n", allowlistPath)
		os.Exit(1)
	}

	fmt.Printf("json wrapper check passed (%d allowlisted direct encoding/json calls)\n", len(findings))
}

func scan(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || shouldSkipFile(path) {
			return nil
		}

		fileFindings, err := scanFile(root, path)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func shouldSkipDir(path string, name string) bool {
	if path == "." || path == "" {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch filepath.ToSlash(path) {
	case "web", "electron/node_modules", "logs", "bin", "upload", "data", "home":
		return true
	}
	return false
}

func shouldSkipFile(path string) bool {
	slashPath := filepath.ToSlash(path)
	return slashPath == "common/json.go" || strings.HasPrefix(slashPath, "tools/jsonwrapcheck/")
}

func scanFile(root string, path string) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	aliases := jsonAliases(file)
	if len(aliases) == 0 {
		return nil, nil
	}

	functions := functionRanges(file)
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = path
	}
	relPath = filepath.ToSlash(relPath)

	var findings []finding
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if !ok || !aliases[ident.Name] {
			return true
		}
		if _, forbidden := forbiddenJSONCalls[selector.Sel.Name]; !forbidden {
			return true
		}

		expr := formatNode(fset, call)
		sum := sha256.Sum256([]byte(expr))
		findings = append(findings, finding{
			Path:     relPath,
			Line:     fset.Position(call.Pos()).Line,
			Function: functionNameAt(call.Pos(), functions),
			Selector: selector.Sel.Name,
			ExprHash: fmt.Sprintf("%x", sum),
			Expr:     expr,
		})
		return true
	})

	return findings, nil
}

func jsonAliases(file *ast.File) map[string]bool {
	aliases := map[string]bool{}
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, "\"") != "encoding/json" {
			continue
		}
		name := "json"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		aliases[name] = true
	}
	return aliases
}

type functionRange struct {
	name  string
	start token.Pos
	end   token.Pos
}

func functionRanges(file *ast.File) []functionRange {
	var ranges []functionRange
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ranges = append(ranges, functionRange{
			name:  functionName(fn),
			start: fn.Pos(),
			end:   fn.End(),
		})
	}
	return ranges
}

func functionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return fmt.Sprintf("%s.%s", receiverName(fn.Recv.List[0].Type), fn.Name.Name)
}

func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func functionNameAt(pos token.Pos, ranges []functionRange) string {
	for _, item := range ranges {
		if item.start <= pos && pos <= item.end {
			return item.name
		}
	}
	return "<file-scope>"
}

func formatNode(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return fmt.Sprintf("%T", node)
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func (f finding) key() string {
	return strings.Join([]string{f.Path, f.Function, f.Selector, f.ExprHash}, "|")
}

func loadAllowlist(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	allowlist := map[string]int{}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(strings.Split(line, "|")) != 4 {
			return nil, fmt.Errorf("%s:%d: invalid allowlist entry %q", path, lineNumber+1, line)
		}
		allowlist[line]++
	}
	return allowlist, nil
}

func sortFindings(findings []finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].key() < findings[j].key()
	})
}

func printAllowlist(findings []finding) {
	fmt.Println("# Existing direct encoding/json function calls.")
	fmt.Println("#")
	fmt.Println("# AGENTS.md requires business code to use common/json.go wrappers for JSON")
	fmt.Println("# marshal/unmarshal operations. This baseline freezes existing call sites")
	fmt.Println("# so new direct calls fail CI while old code is migrated incrementally.")
	fmt.Println("#")
	fmt.Println("# Format: <path>|<enclosing function>|<json selector>|<sha256(normalized call)>")
	for _, item := range findings {
		fmt.Println(item.key())
	}
}
