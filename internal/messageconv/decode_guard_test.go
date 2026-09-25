package messageconv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Persisted rows (bot_history_messages, discuss rows, fork snapshots) hold the
// stored content shape, which differs from the SDK's JSON: arguments are
// objects, outputs are values, and Memoh annotations are nested objects under
// providerMetadata. Decoding a row with encoding/json into sdk.Message drops
// or fails on all of that, so every reader goes through this package or the
// history codec. The files listed here decode stream envelopes or in-memory
// snapshots that are SDK JSON by construction.
var sdkMessageDecodeAllowlist = map[string]string{
	"internal/agent/application/service_stream.go":   "stream envelope Messages",
	"internal/agent/application/turn_discuss.go":     "terminal stream event Messages",
	"internal/agent/runtime/native/spawn_adapter.go": "terminal stream event Messages",
	"internal/agent/tool/types.go":                   "in-memory context snapshot",
	"internal/messageconv/messageconv.go":            "the codec itself",
}

func TestNoDirectSDKMessageDecodeOutsideCodec(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, allowed := sdkMessageDecodeAllowlist[rel]; allowed {
			return nil
		}
		for _, line := range directSDKMessageDecodes(t, path) {
			violations = append(violations, rel+":"+line)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("persisted rows must be typed through messageconv or the history codec, found direct sdk.Message decodes:\n%s", strings.Join(violations, "\n"))
	}
}

// directSDKMessageDecodes reports json.Unmarshal calls whose target is a
// variable declared as sdk.Message, *sdk.Message or []sdk.Message in the
// same function.
func directSDKMessageDecodes(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	sdkAlias := ""
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == "github.com/felinics/twilight/sdk" {
			sdkAlias = "sdk"
			if imp.Name != nil {
				sdkAlias = imp.Name.Name
			}
		}
	}
	if sdkAlias == "" {
		return nil
	}
	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		targets := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.ValueSpec:
				if isSDKMessageType(decl.Type, sdkAlias) {
					for _, name := range decl.Names {
						targets[name.Name] = true
					}
				}
			case *ast.AssignStmt:
				for i, rhs := range decl.Rhs {
					if i >= len(decl.Lhs) {
						break
					}
					ident, ok := decl.Lhs[i].(*ast.Ident)
					if !ok {
						continue
					}
					if lit, ok := rhs.(*ast.CompositeLit); ok && isSDKMessageType(lit.Type, sdkAlias) {
						targets[ident.Name] = true
					}
					if call, ok := rhs.(*ast.CallExpr); ok {
						if fun, ok := call.Fun.(*ast.Ident); ok && fun.Name == "make" && len(call.Args) > 0 && isSDKMessageType(call.Args[0], sdkAlias) {
							targets[ident.Name] = true
						}
					}
				}
			}
			return true
		})
		if len(targets) == 0 {
			return false
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Unmarshal" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "json" {
				return true
			}
			unary, ok := call.Args[1].(*ast.UnaryExpr)
			if !ok || unary.Op != token.AND {
				return true
			}
			if ident, ok := unary.X.(*ast.Ident); ok && targets[ident.Name] {
				found = append(found, fset.Position(call.Pos()).String()[strings.LastIndex(fset.Position(call.Pos()).String(), ":")+1:]+" "+ident.Name)
			}
			return true
		})
		return false
	})
	return found
}

func isSDKMessageType(expr ast.Expr, sdkAlias string) bool {
	switch typ := expr.(type) {
	case *ast.ArrayType:
		return isSDKMessageType(typ.Elt, sdkAlias)
	case *ast.StarExpr:
		return isSDKMessageType(typ.X, sdkAlias)
	case *ast.SelectorExpr:
		pkg, ok := typ.X.(*ast.Ident)
		return ok && pkg.Name == sdkAlias && typ.Sel.Name == "Message"
	}
	return false
}
