package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	sdkImportPath            = "github.com/memohai/twilight-ai/sdk"
	toolsImportPath          = "github.com/memohai/memoh/internal/agent/tools"
	mcpImportPath            = "github.com/memohai/memoh/internal/mcp"
	memoryAdaptersImportPath = "github.com/memohai/memoh/internal/memory/adapters"
)

func TestBuiltInToolNamesAreUnique(t *testing.T) {
	t.Parallel()

	constants, mapKeys := toolNamesGoConstants(t)
	seen := make(map[string]ToolName)
	for name := range builtInToolNames {
		raw := name.String()
		if raw == "" {
			t.Fatal("built-in tool name must not be empty")
		}
		if prev, ok := seen[raw]; ok {
			t.Fatalf("duplicate built-in tool name %q for %s and %s", raw, prev, name)
		}
		seen[raw] = name
	}
	for name := range constants {
		if _, ok := mapKeys[name]; !ok {
			t.Fatalf("tool constant %s must be listed in builtInToolNames", name)
		}
	}
	for name := range mapKeys {
		if _, ok := constants[name]; !ok {
			t.Fatalf("builtInToolNames contains %s, but names.go does not declare that tool constant", name)
		}
	}
}

func TestBuiltInToolNamesUseConstantsInProviders(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob tools files: %v", err)
	}
	constants, _ := toolNamesGoConstants(t)

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "names.go" {
			continue
		}
		src := readGoSource(t, file)
		checkGoFileForToolNames(t, file, src, constants)
	}
}

func TestAvailableToolsRefsUseConstantsAcrossAgent(t *testing.T) {
	t.Parallel()

	files := walkGoFiles(t, repoRootForTest(t))
	packageFactories := availableToolsFactoriesByPackage(t, files)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src := readGoSource(t, file)
		checkGoFileForAvailableToolsUsage(t, file, src, packageFactories[packageKeyForFile(t, file)])
	}
}

func TestMemoryAdapterMCPToolNamesUseConstants(t *testing.T) {
	t.Parallel()

	files := globGoFiles(t, "../../memory/adapters/*.go", "../../memory/adapters/*/*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src := readGoSource(t, file)
		checkGoFileForMCPToolNames(t, file, src)
	}
}

func readGoSource(t *testing.T, file string) []byte {
	t.Helper()
	src, err := os.ReadFile(file) //nolint:gosec // G304: test reads Go files from fixed repository glob patterns.
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	return src
}

func globGoFiles(t *testing.T, patterns ...string) []string {
	t.Helper()
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		files = append(files, matches...)
	}
	return files
}

func walkGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipGuardDir(path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func packageKeyForFile(t *testing.T, file string) string {
	t.Helper()
	abs, err := filepath.Abs(file)
	if err != nil {
		t.Fatalf("resolve file path %s: %v", file, err)
	}
	return filepath.Dir(abs)
}

type availableToolsFactorySet struct {
	names map[string]struct{}
	info  map[string]map[int]struct{}
	raw   map[string]string
}

func availableToolsFactoriesByPackage(t *testing.T, files []string) map[string]availableToolsFactorySet {
	t.Helper()
	out := map[string]availableToolsFactorySet{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src := readGoSource(t, file)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		toolsAliases := importAliases(parsed, toolsImportPath, "tools")
		localToolsPackage := parsed.Name.Name == "tools"
		factoryNames := availableToolsFactoryNames(parsed, toolsAliases, localToolsPackage, availableToolsTypeAliases(parsed))
		factoryInfo := availableToolsFactoryInfo(parsed, toolsAliases, localToolsPackage, availableToolsTypeAliases(parsed))
		rawNames := rawToolNameConstObjectNames(file, parsed, toolsAliases, localToolsPackage)
		if len(factoryNames) == 0 && len(rawNames) == 0 {
			continue
		}
		key := packageKeyForFile(t, file)
		set := out[key]
		if set.names == nil {
			set.names = map[string]struct{}{}
		}
		if set.info == nil {
			set.info = map[string]map[int]struct{}{}
		}
		if set.raw == nil {
			set.raw = map[string]string{}
		}
		for name := range factoryNames {
			set.names[name] = struct{}{}
		}
		for name, indexes := range factoryInfo {
			set.info[name] = indexes
		}
		for name, raw := range rawNames {
			set.raw[name] = raw
		}
		out[key] = set
	}
	return out
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if filepath.Base(root) == "internal" {
		root = filepath.Dir(root)
	}
	return root
}

func shouldSkipGuardDir(path, name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "coverage":
		return true
	}
	clean := filepath.ToSlash(path)
	return strings.Contains(clean, "/internal/db/") && strings.Contains(clean, "/sqlc")
}

func checkGoFileForToolNames(t *testing.T, file string, src []byte, constants map[string]struct{}) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	sdkAliases := importAliases(parsed, sdkImportPath, "sdk")
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if ok {
			if isSDKToolComposite(lit.Type, sdkAliases) {
				checkSDKToolName(t, fset, file, lit, constants, allowDynamicSDKDescriptorName(file))
			}
			if isSDKToolSlice(lit.Type, sdkAliases) {
				for _, elt := range lit.Elts {
					child, ok := elt.(*ast.CompositeLit)
					if ok && child.Type == nil {
						checkSDKToolName(t, fset, file, child, constants, allowDynamicSDKDescriptorName(file))
					}
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok && isSDKNewToolCall(call, sdkAliases) {
			if err := checkSDKNewToolNameValue(call, constants); err != nil {
				t.Fatalf("%s:%d registers sdk.NewTool with invalid name: %v", file, fset.Position(call.Pos()).Line, err)
			}
		}
		return true
	})
}

func checkGoFileForAvailableToolsUsage(t *testing.T, file string, src []byte, packageFactories availableToolsFactorySet) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	toolsAliases := importAliases(parsed, toolsImportPath, "tools")
	localToolsPackage := parsed.Name.Name == "tools"
	aliasNames := availableToolsTypeAliases(parsed)
	factoryNames := availableToolsFactoryNames(parsed, toolsAliases, localToolsPackage, aliasNames)
	factoryInfo := availableToolsFactoryInfo(parsed, toolsAliases, localToolsPackage, aliasNames)
	for name := range packageFactories.names {
		factoryNames[name] = struct{}{}
	}
	for name, indexes := range packageFactories.info {
		factoryInfo[name] = indexes
	}
	availableIndex := availableToolsUsageIndexForFileWithFactories(parsed, toolsAliases, localToolsPackage, factoryNames, factoryInfo)
	rawNameState := newRawToolNameState(parsed, toolsAliases, localToolsPackage)
	rawNameState.packageRawNames = packageFactories.raw
	ast.Inspect(parsed, func(n ast.Node) bool {
		rawNameState.update(n)
		if call, ok := n.(*ast.CallExpr); ok {
			checkRawToolNameConversion(t, fset, file, call, rawNameState.names, toolsAliases, localToolsPackage)
			checkAvailableToolsCall(t, fset, file, call, availableIndex, rawNameState.names, rawNameState.sliceNames, toolsAliases, factoryNames, localToolsPackage)
			checkAvailableToolsPackageRawNameCall(t, fset, file, call, availableIndex, rawNameState.packageRawNames, toolsAliases, factoryNames, localToolsPackage)
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			checkAvailableToolsBackingSelector(t, fset, file, sel, availableIndex, parsed, toolsAliases, factoryNames, localToolsPackage)
		}
		if lit, ok := n.(*ast.CompositeLit); ok {
			checkAvailableToolsBackingLiteral(t, fset, file, lit, parsed, toolsAliases, localToolsPackage)
		}
		return true
	})
}

func checkRawToolNameConversion(t *testing.T, fset *token.FileSet, file string, call *ast.CallExpr, rawConstNames map[token.Pos]string, toolsAliases map[string]struct{}, localToolsPackage bool) {
	t.Helper()

	raw, ok := rawToolNameConversion(call, rawConstNames, toolsAliases, localToolsPackage)
	if !ok {
		return
	}
	t.Fatalf("%s:%d converts string literal %q to ToolName; use a central ToolName constant", file, fset.Position(call.Pos()).Line, raw)
}

func checkAvailableToolsCall(t *testing.T, fset *token.FileSet, file string, call *ast.CallExpr, availableIndex availableToolsUsageIndex, rawConstNames map[token.Pos]string, toolNameSliceNames map[token.Pos]struct{}, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) {
	t.Helper()

	method, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, toolNameSliceNames, toolsAliases, factoryNames, localToolsPackage)
	if !ok {
		return
	}
	t.Fatalf("%s:%d uses available.%s with string literal %q; use a ToolName constant", file, fset.Position(call.Pos()).Line, method, raw)
}

func checkAvailableToolsPackageRawNameCall(t *testing.T, fset *token.FileSet, file string, call *ast.CallExpr, availableIndex availableToolsUsageIndex, packageRawNames map[string]string, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) {
	t.Helper()

	if len(packageRawNames) == 0 {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if sel.Sel.Name != "Has" && sel.Sel.Name != "Ref" && sel.Sel.Name != "Refs" {
		return
	}
	if !isAvailableToolsBackingReceiver(sel.X, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
		return
	}
	for _, arg := range call.Args {
		if raw, ok := packageRawNameArg(arg, packageRawNames, toolsAliases, localToolsPackage); ok {
			t.Fatalf("%s:%d uses available.%s with string literal %q; use a ToolName constant", file, fset.Position(call.Pos()).Line, sel.Sel.Name, raw)
		}
	}
}

func packageRawNameArg(expr ast.Expr, packageRawNames map[string]string, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	expr = unwrapParenExpr(expr)
	if slice, ok := expr.(*ast.SliceExpr); ok {
		return packageRawNameArg(slice.X, packageRawNames, toolsAliases, localToolsPackage)
	}
	if lit, ok := expr.(*ast.CompositeLit); ok && isToolNameSliceType(lit.Type, toolsAliases, localToolsPackage) {
		for _, elt := range lit.Elts {
			if raw, ok := packageRawNameArg(elt, packageRawNames, toolsAliases, localToolsPackage); ok {
				return raw, true
			}
		}
		return "", false
	}
	if ident, ok := expr.(*ast.Ident); ok && ident.Obj == nil {
		raw, ok := packageRawNames[ident.Name]
		return raw, ok
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if isToolNameType(call.Fun, toolsAliases, localToolsPackage) && len(call.Args) == 1 {
			return packageRawNameArg(call.Args[0], packageRawNames, toolsAliases, localToolsPackage)
		}
		if fun, ok := unwrapParenExpr(call.Fun).(*ast.Ident); ok && fun.Name == "append" && len(call.Args) > 0 {
			for _, arg := range call.Args {
				if raw, ok := packageRawNameArg(arg, packageRawNames, toolsAliases, localToolsPackage); ok {
					return raw, true
				}
			}
		}
	}
	return "", false
}

func availableToolsStringLiteralCall(call *ast.CallExpr, availableIndex availableToolsUsageIndex, rawConstNames map[token.Pos]string, toolNameSliceNames map[token.Pos]struct{}, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) (method string, raw string, ok bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	if sel.Sel.Name != "Has" && sel.Sel.Name != "Ref" && sel.Sel.Name != "Refs" {
		return "", "", false
	}
	if !isAvailableToolsBackingReceiver(sel.X, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
		return "", "", false
	}
	for _, arg := range call.Args {
		arg = unwrapParenExpr(arg)
		if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			raw, _ := strconv.Unquote(lit.Value)
			return sel.Sel.Name, raw, true
		}
		if raw, ok := rawToolNameConversion(arg, rawConstNames, toolsAliases, localToolsPackage); ok {
			return sel.Sel.Name, raw, true
		}
		if ident, ok := arg.(*ast.Ident); ok {
			if raw, ok := rawConstNameForIdent(ident, rawConstNames); ok {
				return sel.Sel.Name, raw, true
			}
		}
		if raw, ok := rawToolNameSliceLiteralValue(arg, rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
			return sel.Sel.Name, raw, true
		}
		if selector, ok := arg.(*ast.SelectorExpr); ok && !isCentralToolNameSelector(selector, toolsAliases) {
			return sel.Sel.Name, selectorString(selector), true
		}
	}
	return "", "", false
}

func rawToolNameSliceLiteralValue(expr ast.Expr, rawConstNames map[token.Pos]string, toolNameSliceNames map[token.Pos]struct{}, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	expr = unwrapParenExpr(expr)
	if slice, ok := expr.(*ast.SliceExpr); ok {
		if ident, ok := unwrapParenExpr(slice.X).(*ast.Ident); ok {
			return rawConstNameForIdent(ident, rawConstNames)
		}
		return rawToolNameSliceLiteralValue(slice.X, rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage)
	}
	if raw, ok := rawToolNameAppendValue(expr, rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
		return raw, true
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	if !isToolNameSliceType(lit.Type, toolsAliases, localToolsPackage) {
		return "", false
	}
	for _, elt := range lit.Elts {
		elt = unwrapParenExpr(elt)
		if raw, ok := rawStringLiteral(elt); ok {
			return raw, true
		}
		if raw, ok := rawToolNameExprValue(elt, nil, toolsAliases, localToolsPackage); ok {
			return raw, true
		}
		if ident, ok := elt.(*ast.Ident); ok {
			if raw, ok := rawConstNameForIdent(ident, rawConstNames); ok {
				return raw, true
			}
		}
		if raw, ok := rawToolNameConversion(elt, rawConstNames, toolsAliases, localToolsPackage); ok {
			return raw, true
		}
	}
	return "", false
}

func rawToolNameAppendValue(expr ast.Expr, rawConstNames map[token.Pos]string, toolNameSliceNames map[token.Pos]struct{}, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	call, ok := unwrapParenExpr(expr).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	fun, ok := unwrapParenExpr(call.Fun).(*ast.Ident)
	if !ok || fun.Name != "append" {
		return "", false
	}
	if ident, ok := unwrapParenExpr(call.Args[0]).(*ast.Ident); ok {
		if raw, ok := rawConstNameForIdent(ident, rawConstNames); ok {
			return raw, true
		}
	}
	if raw, ok := rawToolNameSliceLiteralValue(call.Args[0], rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
		return raw, true
	}
	if !isToolNameSliceExpr(call.Args[0], rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage) {
		return "", false
	}
	for _, arg := range call.Args[1:] {
		if raw, ok := rawToolNameSliceLiteralValue(arg, rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
			return raw, true
		}
		if raw, ok := rawStringLiteral(arg); ok {
			return raw, true
		}
		if raw, ok := rawToolNameConversion(arg, rawConstNames, toolsAliases, localToolsPackage); ok {
			return raw, true
		}
		if ident, ok := unwrapParenExpr(arg).(*ast.Ident); ok {
			if raw, ok := rawConstNameForIdent(ident, rawConstNames); ok {
				return raw, true
			}
		}
	}
	return "", false
}

func isToolNameSliceExpr(expr ast.Expr, rawConstNames map[token.Pos]string, toolNameSliceNames map[token.Pos]struct{}, toolsAliases map[string]struct{}, localToolsPackage bool) bool {
	expr = unwrapParenExpr(expr)
	if lit, ok := expr.(*ast.CompositeLit); ok {
		return isToolNameSliceType(lit.Type, toolsAliases, localToolsPackage)
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if _, ok := rawConstNameForIdent(ident, rawConstNames); ok {
			return true
		}
		return toolNameSliceNameForIdent(ident, toolNameSliceNames)
	}
	if slice, ok := expr.(*ast.SliceExpr); ok {
		return isToolNameSliceExpr(slice.X, rawConstNames, toolNameSliceNames, toolsAliases, localToolsPackage)
	}
	return false
}

func rawStringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := unwrapParenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	raw, _ := strconv.Unquote(lit.Value)
	return raw, true
}

func isToolNameSliceType(expr ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool) bool {
	switch value := unwrapParenExpr(expr).(type) {
	case *ast.ArrayType:
		return isToolNameType(value.Elt, toolsAliases, localToolsPackage)
	default:
		return false
	}
}

func rawToolNameConversion(expr ast.Expr, rawConstNames map[token.Pos]string, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	expr = unwrapParenExpr(expr)
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if !localToolsPackage || fun.Name != "ToolName" {
			return "", false
		}
	case *ast.SelectorExpr:
		if fun.Sel.Name != "ToolName" {
			return "", false
		}
		pkg, ok := fun.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if _, ok := toolsAliases[pkg.Name]; !ok {
			return "", false
		}
	default:
		return "", false
	}
	arg := unwrapParenExpr(call.Args[0])
	lit, ok := arg.(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		raw, _ := strconv.Unquote(lit.Value)
		return raw, true
	}
	if ident, ok := arg.(*ast.Ident); ok {
		return rawConstNameForIdent(ident, rawConstNames)
	}
	return "", false
}

func unwrapParenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func rawConstNameForIdent(ident *ast.Ident, rawConstNames map[token.Pos]string) (string, bool) {
	if ident == nil || ident.Obj == nil {
		return "", false
	}
	if ident.Obj.Kind != ast.Con && ident.Obj.Kind != ast.Var {
		return "", false
	}
	spec, ok := ident.Obj.Decl.(*ast.ValueSpec)
	if !ok {
		if assign, ok := ident.Obj.Decl.(*ast.AssignStmt); ok {
			for _, lhs := range assign.Lhs {
				name, ok := lhs.(*ast.Ident)
				if !ok || name.Name != ident.Name {
					continue
				}
				raw, ok := rawConstNames[name.Pos()]
				return raw, ok
			}
		}
		return "", false
	}
	for _, name := range spec.Names {
		if name.Name == ident.Name {
			raw, ok := rawConstNames[name.Pos()]
			return raw, ok
		}
	}
	return "", false
}

func checkAvailableToolsBackingSelector(t *testing.T, fset *token.FileSet, file string, sel *ast.SelectorExpr, availableIndex availableToolsUsageIndex, parsed *ast.File, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) {
	t.Helper()

	if sel.Sel.Name != "names" {
		return
	}
	if !isAvailableToolsBackingReceiver(sel.X, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
		return
	}
	if allowedAvailableToolsBackingAccess(file, parsed, sel.Pos()) {
		return
	}
	t.Fatalf("%s:%d accesses AvailableTools backing set directly; use Has/Ref/Refs with a ToolName constant", file, fset.Position(sel.Pos()).Line)
}

func checkAvailableToolsBackingLiteral(t *testing.T, fset *token.FileSet, file string, lit *ast.CompositeLit, parsed *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) {
	t.Helper()

	aliasNames := availableToolsTypeAliases(parsed)
	if !isDirectAvailableToolsType(lit.Type, toolsAliases, localToolsPackage, aliasNames) {
		return
	}
	if allowedAvailableToolsBackingAccess(file, parsed, lit.Pos()) {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("%s:%d constructs AvailableTools backing set directly; use NewAvailableTools", file, fset.Position(elt.Pos()).Line)
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && key.Name == "names" {
			t.Fatalf("%s:%d constructs AvailableTools backing set directly; use NewAvailableTools", file, fset.Position(key.Pos()).Line)
		}
	}
}

type availableToolsUsageIndex struct {
	names        map[token.Pos]struct{}
	varTypes     map[token.Pos]string
	structFields map[string]map[string]struct{}
	factoryNames map[string]struct{}
	factoryInfo  map[string]map[int]struct{}
	aliases      map[string]ast.Expr
}

func (idx availableToolsUsageIndex) hasAvailableName(ident *ast.Ident) bool {
	if ident == nil || ident.Obj == nil {
		return false
	}
	_, ok := idx.names[ident.Obj.Pos()]
	return ok
}

func (idx availableToolsUsageIndex) availableStructField(receiver ast.Expr, field string) bool {
	ident, ok := receiver.(*ast.Ident)
	if !ok || ident.Obj == nil {
		return false
	}
	typeName, ok := idx.varTypes[ident.Obj.Pos()]
	if !ok {
		return false
	}
	fields := idx.structFields[typeName]
	if fields == nil {
		return false
	}
	_, ok = fields[field]
	return ok
}

func availableToolsUsageIndexForFile(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) availableToolsUsageIndex {
	aliases := availableToolsTypeAliases(file)
	return availableToolsUsageIndexForFileWithFactories(
		file,
		toolsAliases,
		localToolsPackage,
		availableToolsFactoryNames(file, toolsAliases, localToolsPackage, aliases),
		availableToolsFactoryInfo(file, toolsAliases, localToolsPackage, aliases),
	)
}

func availableToolsUsageIndexForFileWithFactories(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool, factoryNames map[string]struct{}, factoryInfo map[string]map[int]struct{}) availableToolsUsageIndex {
	idx := availableToolsUsageIndex{
		names:        map[token.Pos]struct{}{},
		varTypes:     map[token.Pos]string{},
		structFields: map[string]map[string]struct{}{},
		factoryNames: map[string]struct{}{},
		factoryInfo:  map[string]map[int]struct{}{},
	}
	idx.aliases = availableToolsTypeAliases(file)
	idx.factoryNames = factoryNames
	idx.factoryInfo = factoryInfo
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			addAvailableToolsFields(idx.names, d.Recv, toolsAliases, localToolsPackage, idx.aliases)
			addValueTypeNames(idx.varTypes, d.Recv)
			if d.Type != nil {
				addAvailableToolsFields(idx.names, d.Type.Params, toolsAliases, localToolsPackage, idx.aliases)
				addValueTypeNames(idx.varTypes, d.Type.Params)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch valueSpec := spec.(type) {
				case *ast.ValueSpec:
					addAvailableToolsValueNames(idx.names, valueSpec.Names, valueSpec.Type, toolsAliases, localToolsPackage, idx.aliases)
					addValueTypeNamesForIdents(idx.varTypes, valueSpec.Names, valueSpec.Type)
				case *ast.TypeSpec:
					structType, ok := valueSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					addAvailableToolsFields(idx.names, structType.Fields, toolsAliases, localToolsPackage, idx.aliases)
					idx.structFields[valueSpec.Name.Name] = availableToolsStructFields(structType.Fields, toolsAliases, localToolsPackage, idx.aliases)
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if gen, ok := n.(*ast.GenDecl); ok && gen.Tok == token.VAR {
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if addAvailableToolsValueNames(idx.names, valueSpec.Names, valueSpec.Type, toolsAliases, localToolsPackage, idx.aliases) {
					addValueTypeNamesForIdents(idx.varTypes, valueSpec.Names, valueSpec.Type)
					continue
				}
				addValueTypeNamesForIdents(idx.varTypes, valueSpec.Names, valueSpec.Type)
				if len(valueSpec.Values) == 1 {
					for i, name := range valueSpec.Names {
						if isAvailableToolsMultiReturnAt(valueSpec.Values[0], i, idx, toolsAliases, factoryNames, localToolsPackage, idx.aliases) {
							idx.names[name.Pos()] = struct{}{}
						}
					}
					continue
				}
				for i, value := range valueSpec.Values {
					if !isAvailableToolsExpr(value, idx.names, idx, toolsAliases, factoryNames, localToolsPackage, idx.aliases) || i >= len(valueSpec.Names) {
						continue
					}
					idx.names[valueSpec.Names[i].Pos()] = struct{}{}
				}
			}
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Rhs) == 1 {
			for i, lhs := range assign.Lhs {
				name, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if isAvailableToolsMultiReturnAt(assign.Rhs[0], i, idx, toolsAliases, factoryNames, localToolsPackage, idx.aliases) {
					idx.names[name.Pos()] = struct{}{}
				}
			}
		}
		for i, rhs := range assign.Rhs {
			if i >= len(assign.Lhs) {
				continue
			}
			name, ok := assign.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			if isAvailableToolsExpr(rhs, idx.names, idx, toolsAliases, factoryNames, localToolsPackage, idx.aliases) {
				idx.names[name.Pos()] = struct{}{}
				continue
			}
			if isAvailableToolsMultiReturnAt(rhs, i, idx, toolsAliases, factoryNames, localToolsPackage, idx.aliases) {
				idx.names[name.Pos()] = struct{}{}
				continue
			}
			if typeName := namedTypeName(rhs); typeName != "" {
				idx.varTypes[name.Pos()] = typeName
			}
		}
		return true
	})
	return idx
}

func addValueTypeNames(varTypes map[token.Pos]string, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		addValueTypeNamesForIdents(varTypes, field.Names, field.Type)
	}
}

func addValueTypeNamesForIdents(varTypes map[token.Pos]string, idents []*ast.Ident, typ ast.Expr) {
	typeName := namedTypeName(typ)
	if typeName == "" {
		return
	}
	for _, ident := range idents {
		varTypes[ident.Pos()] = typeName
	}
}

func availableToolsStructFields(fields *ast.FieldList, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) map[string]struct{} {
	out := map[string]struct{}{}
	if fields == nil {
		return out
	}
	for _, field := range fields.List {
		if !isAvailableToolsType(field.Type, toolsAliases, localToolsPackage, aliases) {
			continue
		}
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				out[name.Name] = struct{}{}
			}
			continue
		}
		if name := embeddedAvailableToolsFieldName(field.Type, toolsAliases, localToolsPackage, aliases); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func namedTypeName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return namedTypeName(value.X)
	case *ast.CompositeLit:
		return namedTypeName(value.Type)
	default:
		return ""
	}
}

func rawConstToolNameValue(spec *ast.ValueSpec, inheritedType ast.Expr, inheritedValues []ast.Expr, index int, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	if !isToolNameType(firstNonNilExpr(spec.Type, inheritedType), toolsAliases, localToolsPackage) && spec.Type != nil {
		return "", false
	}
	if len(spec.Values) == 0 && len(inheritedValues) == 0 {
		return "", false
	}
	values := spec.Values
	if len(values) == 0 {
		values = inheritedValues
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	value := unwrapParenExpr(values[index])
	lit, ok := value.(*ast.BasicLit)
	if ok && lit.Kind == token.STRING && spec.Type == nil && inheritedType == nil {
		raw, _ := strconv.Unquote(lit.Value)
		if IsBuiltInToolName(raw) {
			return raw, true
		}
		return "", false
	}
	if ok && lit.Kind == token.STRING && isToolNameType(firstNonNilExpr(spec.Type, inheritedType), toolsAliases, localToolsPackage) {
		raw, _ := strconv.Unquote(lit.Value)
		return raw, true
	}
	if sel, ok := value.(*ast.SelectorExpr); ok && !isCentralToolNameSelector(sel, toolsAliases) {
		return selectorString(sel), true
	}
	return "", false
}

func rawToolNameVarValue(spec *ast.ValueSpec, index int, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	if len(spec.Values) == 0 {
		return "", false
	}
	if index >= len(spec.Values) {
		index = len(spec.Values) - 1
	}
	return rawToolNameExprValue(spec.Values[index], spec.Type, toolsAliases, localToolsPackage)
}

func rawToolNameExprValue(expr ast.Expr, typ ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool) (string, bool) {
	expr = unwrapParenExpr(expr)
	if raw, ok := rawToolNameConversion(expr, nil, toolsAliases, localToolsPackage); ok {
		return raw, true
	}
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		raw, _ := strconv.Unquote(lit.Value)
		if typ == nil {
			return raw, true
		}
		if isToolNameType(typ, toolsAliases, localToolsPackage) {
			return raw, true
		}
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok && !isCentralToolNameSelector(sel, toolsAliases) {
		return selectorString(sel), true
	}
	return "", false
}

func firstNonNilExpr(a, b ast.Expr) ast.Expr {
	if a != nil {
		return a
	}
	return b
}

func isToolNameType(expr ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool) bool {
	expr = unwrapParenExpr(expr)
	switch value := expr.(type) {
	case *ast.Ident:
		return localToolsPackage && value.Name == "ToolName"
	case *ast.SelectorExpr:
		if value.Sel.Name != "ToolName" {
			return false
		}
		pkg, ok := value.X.(*ast.Ident)
		if !ok {
			return false
		}
		_, ok = toolsAliases[pkg.Name]
		return ok
	default:
		return false
	}
}

func isCentralToolNameSelector(sel *ast.SelectorExpr, toolsAliases map[string]struct{}) bool {
	if sel == nil || !strings.HasPrefix(sel.Sel.Name, "Tool") {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Obj != nil {
		return false
	}
	_, ok = toolsAliases[pkg.Name]
	return ok
}

func selectorString(sel *ast.SelectorExpr) string {
	if sel == nil {
		return ""
	}
	if pkg, ok := sel.X.(*ast.Ident); ok {
		return pkg.Name + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

func allowedAvailableToolsBackingAccess(file string, parsed *ast.File, pos token.Pos) bool {
	if filepath.Base(file) != "types.go" {
		return false
	}
	if parsed == nil {
		return false
	}
	fn := enclosingFunc(parsed, pos)
	if fn == nil {
		return false
	}
	switch fn.Name.Name {
	case "NewAvailableTools":
		return true
	case "Has", "Ref", "Refs":
		return receiverIsAvailableTools(fn.Recv)
	default:
		return false
	}
}

func enclosingFunc(file *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Pos() <= pos && pos <= fn.End() {
			return fn
		}
	}
	return nil
}

func receiverIsAvailableTools(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	ident, ok := recv.List[0].Type.(*ast.Ident)
	return ok && ident.Name == "AvailableTools"
}

func availableToolsNameObjects(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[token.Pos]struct{} {
	names := map[token.Pos]struct{}{}
	varTypes := map[token.Pos]string{}
	aliases := availableToolsTypeAliases(file)
	factoryNames := availableToolsFactoryNames(file, toolsAliases, localToolsPackage, aliases)
	idx := availableToolsUsageIndex{
		names:        names,
		varTypes:     varTypes,
		structFields: map[string]map[string]struct{}{},
		factoryNames: factoryNames,
		aliases:      aliases,
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			addAvailableToolsFields(names, d.Recv, toolsAliases, localToolsPackage, aliases)
			addValueTypeNames(varTypes, d.Recv)
			if d.Type != nil {
				addAvailableToolsFields(names, d.Type.Params, toolsAliases, localToolsPackage, aliases)
				addValueTypeNames(varTypes, d.Type.Params)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch valueSpec := spec.(type) {
				case *ast.ValueSpec:
					addAvailableToolsValueNames(names, valueSpec.Names, valueSpec.Type, toolsAliases, localToolsPackage, aliases)
					addValueTypeNamesForIdents(varTypes, valueSpec.Names, valueSpec.Type)
				case *ast.TypeSpec:
					structType, ok := valueSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					addAvailableToolsFields(names, structType.Fields, toolsAliases, localToolsPackage, aliases)
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if gen, ok := n.(*ast.GenDecl); ok && gen.Tok == token.VAR {
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if addAvailableToolsValueNames(names, valueSpec.Names, valueSpec.Type, toolsAliases, localToolsPackage, aliases) {
					addValueTypeNamesForIdents(varTypes, valueSpec.Names, valueSpec.Type)
					continue
				}
				addValueTypeNamesForIdents(varTypes, valueSpec.Names, valueSpec.Type)
				for i, value := range valueSpec.Values {
					if !isAvailableToolsExpr(value, names, idx, toolsAliases, factoryNames, localToolsPackage, aliases) || i >= len(valueSpec.Names) {
						continue
					}
					names[valueSpec.Names[i].Pos()] = struct{}{}
				}
			}
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			if !isAvailableToolsExpr(rhs, names, idx, toolsAliases, factoryNames, localToolsPackage, aliases) || i >= len(assign.Lhs) {
				continue
			}
			if name, ok := assign.Lhs[i].(*ast.Ident); ok {
				names[name.Pos()] = struct{}{}
			}
		}
		return true
	})
	return names
}

func addAvailableToolsValueNames(names map[token.Pos]struct{}, idents []*ast.Ident, typ ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	if !isAvailableToolsType(typ, toolsAliases, localToolsPackage, aliases) {
		return false
	}
	for _, name := range idents {
		names[name.Pos()] = struct{}{}
	}
	return true
}

func availableToolNameObjectsForTest(t *testing.T, parsed *ast.File, names ...string) map[token.Pos]struct{} {
	t.Helper()
	want := map[string]struct{}{}
	for _, name := range names {
		want[name] = struct{}{}
	}
	out := map[token.Pos]struct{}{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Obj == nil {
			return true
		}
		if _, ok := want[ident.Name]; ok && ident.Pos() == ident.Obj.Pos() {
			out[ident.Obj.Pos()] = struct{}{}
		}
		return true
	})
	if len(out) != len(want) {
		t.Fatalf("resolved AvailableTools test names = %d, want %d", len(out), len(want))
	}
	return out
}

func rawToolNameConstObjects(filename string, file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[token.Pos]string {
	names := map[token.Pos]string{}
	centralToolNames := centralToolNameConstantsForFile(filename, file)
	ast.Inspect(file, func(n ast.Node) bool {
		gen, ok := n.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			return true
		}
		var currentType ast.Expr
		var inheritedValues []ast.Expr
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if valueSpec.Type != nil {
				currentType = valueSpec.Type
			}
			if len(valueSpec.Values) > 0 {
				inheritedValues = valueSpec.Values
			}
			for i, name := range valueSpec.Names {
				if _, ok := centralToolNames[name.Name]; ok {
					continue
				}
				if raw, ok := rawConstToolNameValue(valueSpec, currentType, inheritedValues, i, toolsAliases, localToolsPackage); ok {
					names[name.Pos()] = raw
				}
			}
		}
		return false
	})
	return names
}

func rawToolNameConstObjectNames(filename string, file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[string]string {
	names := map[string]string{}
	centralToolNames := centralToolNameConstantsForFile(filename, file)
	ast.Inspect(file, func(n ast.Node) bool {
		gen, ok := n.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			return true
		}
		var currentType ast.Expr
		var inheritedValues []ast.Expr
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if valueSpec.Type != nil {
				currentType = valueSpec.Type
			}
			if len(valueSpec.Values) > 0 {
				inheritedValues = valueSpec.Values
			}
			for i, name := range valueSpec.Names {
				if _, ok := centralToolNames[name.Name]; ok {
					continue
				}
				if raw, ok := rawConstToolNameValue(valueSpec, currentType, inheritedValues, i, toolsAliases, localToolsPackage); ok {
					names[name.Name] = raw
				}
			}
		}
		return false
	})
	return names
}

func centralToolNameConstantsForFile(filename string, file *ast.File) map[string]struct{} {
	if filepath.Base(filename) != "names.go" || file.Name == nil || file.Name.Name != "tools" {
		return nil
	}
	constants := map[string]struct{}{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range valueSpec.Names {
				if strings.HasPrefix(name.Name, "Tool") {
					constants[name.Name] = struct{}{}
				}
			}
		}
	}
	return constants
}

func rawToolNameNameObjects(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[token.Pos]string {
	names := rawToolNameConstObjects("", file, toolsAliases, localToolsPackage)
	toolNameSliceNames := toolNameSliceNameObjects(file, toolsAliases, localToolsPackage)
	toolNameVars := toolNameVarObjects(file, toolsAliases, localToolsPackage)
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok != token.VAR {
				return true
			}
			for _, spec := range node.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if raw, ok := rawToolNameVarValue(valueSpec, i, toolsAliases, localToolsPackage); ok {
						names[name.Pos()] = raw
						continue
					}
					if i < len(valueSpec.Values) {
						if raw, ok := rawToolNameSliceLiteralValue(valueSpec.Values[i], names, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
							names[name.Pos()] = raw
						}
					}
				}
			}
			return false
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE && node.Tok != token.ASSIGN {
				return true
			}
			for i, lhs := range node.Lhs {
				name, ok := lhs.(*ast.Ident)
				if !ok || i >= len(node.Rhs) {
					continue
				}
				pos := objectPosForIdent(name, names)
				var typ ast.Expr
				if _, ok := toolNameVars[pos]; ok {
					typ = toolNameIdentForPackage(toolsAliases, localToolsPackage)
				}
				if raw, ok := rawToolNameExprValue(node.Rhs[i], typ, toolsAliases, localToolsPackage); ok {
					names[pos] = raw
					continue
				}
				if raw, ok := rawToolNameSliceLiteralValue(node.Rhs[i], names, toolNameSliceNames, toolsAliases, localToolsPackage); ok {
					names[pos] = raw
					continue
				}
				if node.Tok == token.ASSIGN {
					delete(names, pos)
				}
			}
		}
		return true
	})
	return names
}

type rawToolNameState struct {
	names             map[token.Pos]string
	packageRawNames   map[string]string
	sliceNames        map[token.Pos]struct{}
	toolNameVars      map[token.Pos]struct{}
	toolsAliases      map[string]struct{}
	localToolsPackage bool
}

func newRawToolNameState(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) *rawToolNameState {
	return &rawToolNameState{
		names:             rawToolNameConstObjects("", file, toolsAliases, localToolsPackage),
		packageRawNames:   map[string]string{},
		sliceNames:        toolNameSliceNameObjects(file, toolsAliases, localToolsPackage),
		toolNameVars:      toolNameVarObjects(file, toolsAliases, localToolsPackage),
		toolsAliases:      toolsAliases,
		localToolsPackage: localToolsPackage,
	}
}

func (s *rawToolNameState) update(n ast.Node) {
	switch node := n.(type) {
	case *ast.GenDecl:
		if node.Tok != token.VAR {
			return
		}
		for _, spec := range node.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				pos := objectPosForIdent(name, s.names)
				if raw, ok := rawToolNameVarValue(valueSpec, i, s.toolsAliases, s.localToolsPackage); ok {
					s.names[pos] = raw
					continue
				}
				if i < len(valueSpec.Values) {
					if raw, ok := rawToolNameSliceLiteralValue(valueSpec.Values[i], s.names, s.sliceNames, s.toolsAliases, s.localToolsPackage); ok {
						s.names[pos] = raw
					}
				}
			}
		}
	case *ast.AssignStmt:
		if node.Tok != token.DEFINE && node.Tok != token.ASSIGN {
			return
		}
		for i, lhs := range node.Lhs {
			name, ok := lhs.(*ast.Ident)
			if !ok || i >= len(node.Rhs) {
				continue
			}
			pos := objectPosForIdent(name, s.names)
			var typ ast.Expr
			if _, ok := s.toolNameVars[pos]; ok {
				typ = toolNameIdentForPackage(s.toolsAliases, s.localToolsPackage)
			}
			if raw, ok := rawToolNameExprValue(node.Rhs[i], typ, s.toolsAliases, s.localToolsPackage); ok {
				s.names[pos] = raw
				continue
			}
			if raw, ok := rawToolNameSliceLiteralValue(node.Rhs[i], s.names, s.sliceNames, s.toolsAliases, s.localToolsPackage); ok {
				s.names[pos] = raw
				continue
			}
			if _, hadRaw := s.names[pos]; node.Tok == token.ASSIGN || hadRaw {
				delete(s.names, pos)
			}
		}
	}
}

func toolNameVarObjects(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[token.Pos]struct{} {
	names := map[token.Pos]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok != token.VAR {
				return true
			}
			for _, spec := range node.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if isToolNameType(valueSpec.Type, toolsAliases, localToolsPackage) {
						names[name.Pos()] = struct{}{}
						continue
					}
					if i < len(valueSpec.Values) {
						if _, ok := rawToolNameExprValue(valueSpec.Values[i], valueSpec.Type, toolsAliases, localToolsPackage); ok {
							names[name.Pos()] = struct{}{}
						}
					}
				}
			}
			return false
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range node.Lhs {
				name, ok := lhs.(*ast.Ident)
				if !ok || i >= len(node.Rhs) {
					continue
				}
				if _, ok := rawToolNameExprValue(node.Rhs[i], nil, toolsAliases, localToolsPackage); ok {
					names[objectPosForIdent(name, names)] = struct{}{}
				}
			}
		}
		return true
	})
	return names
}

func toolNameIdentForPackage(toolsAliases map[string]struct{}, localToolsPackage bool) ast.Expr {
	if localToolsPackage {
		return ast.NewIdent("ToolName")
	}
	for alias := range toolsAliases {
		return &ast.SelectorExpr{X: ast.NewIdent(alias), Sel: ast.NewIdent("ToolName")}
	}
	return nil
}

func toolNameSliceNameObjects(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool) map[token.Pos]struct{} {
	names := map[token.Pos]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok != token.VAR {
				return true
			}
			for _, spec := range node.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if isToolNameSliceType(valueSpec.Type, toolsAliases, localToolsPackage) {
						names[name.Pos()] = struct{}{}
						continue
					}
					if i < len(valueSpec.Values) && isToolNameSliceExpr(valueSpec.Values[i], nil, names, toolsAliases, localToolsPackage) {
						names[name.Pos()] = struct{}{}
					}
				}
			}
			return false
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE && node.Tok != token.ASSIGN {
				return true
			}
			for i, lhs := range node.Lhs {
				name, ok := lhs.(*ast.Ident)
				if !ok || i >= len(node.Rhs) {
					continue
				}
				if isToolNameSliceExpr(node.Rhs[i], nil, names, toolsAliases, localToolsPackage) {
					names[objectPosForIdent(name, names)] = struct{}{}
				}
			}
		}
		return true
	})
	return names
}

func toolNameSliceNameForIdent(ident *ast.Ident, toolNameSliceNames map[token.Pos]struct{}) bool {
	if ident == nil || ident.Obj == nil {
		return false
	}
	_, ok := toolNameSliceNames[ident.Obj.Pos()]
	return ok
}

func objectPosForIdent[T any](ident *ast.Ident, names map[token.Pos]T) token.Pos {
	if ident == nil {
		return token.NoPos
	}
	if ident.Obj != nil {
		return ident.Obj.Pos()
	}
	return ident.Pos()
}

func availableToolsTypeAliases(file *ast.File) map[string]ast.Expr {
	aliases := map[string]ast.Expr{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Assign == token.NoPos {
				continue
			}
			aliases[typeSpec.Name.Name] = typeSpec.Type
		}
	}
	return aliases
}

func availableToolsFactoryNames(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) map[string]struct{} {
	info := availableToolsFactoryInfo(file, toolsAliases, localToolsPackage, aliases)
	names := map[string]struct{}{}
	for name := range info {
		names[name] = struct{}{}
	}
	return names
}

func availableToolsFactoryInfo(file *ast.File, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) map[string]map[int]struct{} {
	info := map[string]map[int]struct{}{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type == nil || fn.Type.Results == nil {
			continue
		}
		indexes := availableToolsReturnIndexes(fn.Type.Results, toolsAliases, localToolsPackage, aliases)
		if len(indexes) == 0 {
			continue
		}
		name := fn.Name.Name
		if receiver := methodReceiverTypeName(fn.Recv); receiver != "" {
			name = methodFactoryKey(receiver, fn.Name.Name)
		}
		info[name] = indexes
	}
	return info
}

func methodFactoryKey(receiver, method string) string {
	if receiver == "" || method == "" {
		return ""
	}
	return receiver + "." + method
}

func methodReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	return namedTypeName(recv.List[0].Type)
}

func availableToolsReturnIndexes(results *ast.FieldList, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) map[int]struct{} {
	indexes := map[int]struct{}{}
	if results == nil {
		return indexes
	}
	index := 0
	for _, field := range results.List {
		if isAvailableToolsType(field.Type, toolsAliases, localToolsPackage, aliases) {
			indexes[index] = struct{}{}
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		index += count
	}
	return indexes
}

func addAvailableToolsFields(names map[token.Pos]struct{}, fields *ast.FieldList, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if !isAvailableToolsType(field.Type, toolsAliases, localToolsPackage, aliases) {
			continue
		}
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				names[name.Pos()] = struct{}{}
			}
			continue
		}
		if embeddedAvailableToolsFieldName(field.Type, toolsAliases, localToolsPackage, aliases) != "" {
			names[field.Type.Pos()] = struct{}{}
		}
	}
}

func embeddedAvailableToolsFieldName(expr ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if localToolsPackage && value.Name == "AvailableTools" {
			return value.Name
		}
		if aliases != nil {
			if target, ok := aliases[value.Name]; ok && isAvailableToolsType(target, toolsAliases, localToolsPackage, aliases) {
				return value.Name
			}
		}
	case *ast.SelectorExpr:
		if value.Sel.Name == "AvailableTools" {
			if pkg, ok := value.X.(*ast.Ident); ok {
				if _, ok := toolsAliases[pkg.Name]; ok {
					return value.Sel.Name
				}
			}
		}
	}
	return ""
}

func isAvailableToolsExpr(expr ast.Expr, knownNames map[token.Pos]struct{}, availableIndex availableToolsUsageIndex, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	if isAvailableToolsFactoryCall(expr, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
		return true
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	if ident.Obj == nil {
		return false
	}
	_, ok = knownNames[ident.Obj.Pos()]
	return ok
}

func isAvailableToolsMultiReturnAt(expr ast.Expr, index int, availableIndex availableToolsUsageIndex, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if !isAvailableToolsFactoryCall(call, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
		return false
	}
	fn := factoryFuncDecl(call)
	if fn == nil || fn.Type == nil {
		if key := availableToolsFactoryCallKey(call, availableIndex, localToolsPackage); key != "" {
			_, ok := availableIndex.factoryInfo[key][index]
			return ok
		}
		return index == 0
	}
	_, ok = availableToolsReturnIndexes(fn.Type.Results, toolsAliases, localToolsPackage, aliases)[index]
	return ok
}

func factoryFuncDecl(call *ast.CallExpr) *ast.FuncDecl {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Obj == nil {
		return nil
	}
	return funcDeclForObject(ident.Obj)
}

func funcDeclForObject(obj *ast.Object) *ast.FuncDecl {
	if obj == nil {
		return nil
	}
	if fn, ok := obj.Decl.(*ast.FuncDecl); ok {
		return fn
	}
	return nil
}

func isAvailableToolsBackingReceiver(expr ast.Expr, availableIndex availableToolsUsageIndex, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		return availableIndex.hasAvailableName(value)
	case *ast.SelectorExpr:
		if isAvailableToolsBackingReceiver(value.X, availableIndex, toolsAliases, factoryNames, localToolsPackage) {
			return true
		}
		return availableIndex.availableStructField(value.X, value.Sel.Name)
	case *ast.IndexExpr:
		return isAvailableToolsBackingReceiver(value.X, availableIndex, toolsAliases, factoryNames, localToolsPackage)
	case *ast.ParenExpr:
		return isAvailableToolsBackingReceiver(value.X, availableIndex, toolsAliases, factoryNames, localToolsPackage)
	case *ast.UnaryExpr:
		return value.Op == token.AND && isAvailableToolsBackingReceiver(value.X, availableIndex, toolsAliases, factoryNames, localToolsPackage)
	case *ast.StarExpr:
		return isAvailableToolsBackingReceiver(value.X, availableIndex, toolsAliases, factoryNames, localToolsPackage)
	case *ast.CallExpr:
		if isNewAvailableToolsCall(value, toolsAliases, localToolsPackage, availableIndex.aliases) {
			return true
		}
		return isAvailableToolsFactoryCall(value, availableIndex, toolsAliases, factoryNames, localToolsPackage)
	case *ast.CompositeLit:
		return isAvailableToolsType(value.Type, toolsAliases, localToolsPackage, availableIndex.aliases)
	default:
		return false
	}
}

func isNewAvailableToolsCall(call *ast.CallExpr, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "new" || len(call.Args) != 1 {
		return false
	}
	return isAvailableToolsType(call.Args[0], toolsAliases, localToolsPackage, aliases)
}

func isDirectAvailableToolsType(expr ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	expr = unwrapParenExpr(expr)
	switch value := expr.(type) {
	case *ast.StarExpr:
		return isDirectAvailableToolsType(value.X, toolsAliases, localToolsPackage, aliases)
	case *ast.Ident:
		if localToolsPackage && value.Name == "AvailableTools" {
			return true
		}
		if aliases != nil {
			if target, ok := aliases[value.Name]; ok {
				return isDirectAvailableToolsType(target, toolsAliases, localToolsPackage, aliases)
			}
		}
		return false
	case *ast.SelectorExpr:
		if value.Sel.Name != "AvailableTools" {
			return false
		}
		pkg, ok := value.X.(*ast.Ident)
		if !ok {
			return false
		}
		_, ok = toolsAliases[pkg.Name]
		return ok
	default:
		return false
	}
}

func isAvailableToolsType(expr ast.Expr, toolsAliases map[string]struct{}, localToolsPackage bool, aliases map[string]ast.Expr) bool {
	expr = unwrapParenExpr(expr)
	switch value := expr.(type) {
	case *ast.ArrayType:
		return isAvailableToolsType(value.Elt, toolsAliases, localToolsPackage, aliases)
	case *ast.MapType:
		return isAvailableToolsType(value.Value, toolsAliases, localToolsPackage, aliases)
	case *ast.StarExpr:
		return isAvailableToolsType(value.X, toolsAliases, localToolsPackage, aliases)
	case *ast.Ident:
		if localToolsPackage && value.Name == "AvailableTools" {
			return true
		}
		if aliases != nil {
			if target, ok := aliases[value.Name]; ok {
				return isAvailableToolsType(target, toolsAliases, localToolsPackage, aliases)
			}
		}
		return false
	case *ast.SelectorExpr:
		if value.Sel.Name != "AvailableTools" {
			return false
		}
		pkg, ok := value.X.(*ast.Ident)
		if !ok {
			return false
		}
		_, ok = toolsAliases[pkg.Name]
		return ok
	default:
		return false
	}
}

func isAvailableToolsFactoryCall(expr ast.Expr, availableIndex availableToolsUsageIndex, toolsAliases map[string]struct{}, factoryNames map[string]struct{}, localToolsPackage bool) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if localToolsPackage && fun.Name == "NewAvailableTools" {
			return true
		}
		if fun.Obj != nil {
			fn := funcDeclForObject(fun.Obj)
			return fn != nil && len(availableToolsReturnIndexes(fn.Type.Results, toolsAliases, localToolsPackage, availableIndex.aliases)) > 0
		}
		key := availableToolsFactoryCallKey(call, availableIndex, localToolsPackage)
		_, ok := factoryNames[key]
		return ok
	case *ast.SelectorExpr:
		if key := availableToolsFactoryCallKey(call, availableIndex, localToolsPackage); key != "" {
			if _, ok := factoryNames[key]; ok {
				return true
			}
		}
		if fun.Sel.Name == "NewAvailableTools" {
			pkg, ok := fun.X.(*ast.Ident)
			if !ok {
				return false
			}
			_, ok = toolsAliases[pkg.Name]
			return ok
		}
		return false
	default:
		return false
	}
}

func availableToolsFactoryCallKey(call *ast.CallExpr, availableIndex availableToolsUsageIndex, localToolsPackage bool) string {
	if call == nil {
		return ""
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if localToolsPackage && fun.Name == "NewAvailableTools" {
			return fun.Name
		}
		if fun.Obj != nil && funcDeclForObject(fun.Obj) == nil {
			return ""
		}
		return fun.Name
	case *ast.SelectorExpr:
		if receiverType := selectorReceiverTypeName(fun.X, availableIndex); receiverType != "" {
			return methodFactoryKey(receiverType, fun.Sel.Name)
		}
		return ""
	default:
		return ""
	}
}

func selectorReceiverTypeName(expr ast.Expr, availableIndex availableToolsUsageIndex) string {
	switch value := unwrapParenExpr(expr).(type) {
	case *ast.Ident:
		if value.Obj == nil {
			return ""
		}
		return availableIndex.varTypes[value.Obj.Pos()]
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			return selectorReceiverTypeName(value.X, availableIndex)
		}
	case *ast.StarExpr:
		return selectorReceiverTypeName(value.X, availableIndex)
	case *ast.CompositeLit:
		return namedTypeName(value.Type)
	}
	return ""
}

func allowDynamicSDKDescriptorName(file string) bool {
	switch filepath.Base(file) {
	case "federation.go", "memory.go":
		return true
	default:
		return false
	}
}

func checkGoFileForMCPToolNames(t *testing.T, file string, src []byte) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	mcpAliases := importAliases(parsed, mcpImportPath, "mcp")
	memoryAdapterAliases := importAliases(parsed, memoryAdaptersImportPath, "adapters")
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if isMCPToolDescriptorComposite(lit.Type, mcpAliases) {
			checkMCPToolDescriptorName(t, fset, file, lit, memoryAdapterAliases)
		}
		if isMCPToolDescriptorSlice(lit.Type, mcpAliases) {
			for _, elt := range lit.Elts {
				child, ok := elt.(*ast.CompositeLit)
				if ok && child.Type == nil {
					checkMCPToolDescriptorName(t, fset, file, child, memoryAdapterAliases)
				}
			}
		}
		return true
	})
}

func importAliases(file *ast.File, importPath, defaultName string) map[string]struct{} {
	aliases := map[string]struct{}{}
	for _, spec := range file.Imports {
		raw, err := strconv.Unquote(spec.Path.Value)
		if err != nil || raw != importPath {
			continue
		}
		if spec.Name == nil {
			aliases[defaultName] = struct{}{}
			continue
		}
		if spec.Name.Name != "" && spec.Name.Name != "." && spec.Name.Name != "_" {
			aliases[spec.Name.Name] = struct{}{}
		}
	}
	return aliases
}

func toolNamesGoConstants(t *testing.T) (map[string]struct{}, map[string]struct{}) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "names.go", nil, 0)
	if err != nil {
		t.Fatalf("parse names.go: %v", err)
	}
	constants := map[string]struct{}{}
	mapKeys := map[string]struct{}{}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		switch gen.Tok {
		case token.CONST:
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				for _, name := range valueSpec.Names {
					if strings.HasPrefix(name.Name, "Tool") {
						constants[name.Name] = struct{}{}
					}
				}
			}
		case token.VAR:
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				if len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "builtInToolNames" || len(valueSpec.Values) != 1 {
					continue
				}
				lit, ok := valueSpec.Values[0].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("builtInToolNames must be a map literal")
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						t.Fatalf("builtInToolNames entries must use keyed tool constants")
					}
					name, ok := kv.Key.(*ast.Ident)
					if !ok {
						t.Fatalf("builtInToolNames key at %s must be a tool constant", fset.Position(kv.Key.Pos()))
					}
					mapKeys[name.Name] = struct{}{}
				}
			}
		}
	}
	return constants, mapKeys
}

func isSDKToolSlice(expr ast.Expr, sdkAliases map[string]struct{}) bool {
	array, ok := expr.(*ast.ArrayType)
	if !ok {
		return false
	}
	return isSDKToolComposite(array.Elt, sdkAliases)
}

func isSDKToolComposite(expr ast.Expr, sdkAliases map[string]struct{}) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Tool" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = sdkAliases[pkg.Name]
	return ok
}

func isMCPToolDescriptorSlice(expr ast.Expr, mcpAliases map[string]struct{}) bool {
	array, ok := expr.(*ast.ArrayType)
	if !ok {
		return false
	}
	return isMCPToolDescriptorComposite(array.Elt, mcpAliases)
}

func isMCPToolDescriptorComposite(expr ast.Expr, mcpAliases map[string]struct{}) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "ToolDescriptor" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = mcpAliases[pkg.Name]
	return ok
}

func checkSDKToolName(t *testing.T, fset *token.FileSet, file string, lit *ast.CompositeLit, constants map[string]struct{}, allowDynamicDescriptorName bool) {
	t.Helper()

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			continue
		}
		if err := checkSDKToolNameValue(kv.Value, constants, allowDynamicDescriptorName); err != nil {
			t.Fatalf("%s:%d registers sdk.Tool with invalid Name: %v", file, fset.Position(kv.Value.Pos()).Line, err)
		}
	}
}

func checkSDKToolNameValue(expr ast.Expr, constants map[string]struct{}, allowDynamicDescriptorName bool) error {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			raw, _ := strconv.Unquote(value.Value)
			return &toolNameError{"string literal " + strconv.Quote(raw) + "; use a central ToolName constant or dynamic descriptor name"}
		}
	case *ast.CallExpr:
		sel, ok := value.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "String" {
			return &toolNameError{"call expression is not ToolName.String()"}
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return &toolNameError{"ToolName.String() receiver must be a central tool constant"}
		}
		if ident.Obj != nil {
			return &toolNameError{ident.Name + ".String() uses a local shadow; use the central ToolName constant"}
		}
		if _, ok := constants[ident.Name]; !ok {
			return &toolNameError{ident.Name + ".String() is not declared in names.go and listed in builtInToolNames"}
		}
		return nil
	case *ast.SelectorExpr:
		if allowDynamicDescriptorName && selectorIsDynamicDescriptorName(value) {
			return nil
		}
		return &toolNameError{"selector expression is not an allowed dynamic descriptor name"}
	}
	return &toolNameError{"use a central ToolName constant or dynamic descriptor name"}
}

func selectorIsDynamicDescriptorName(value *ast.SelectorExpr) bool {
	ident, ok := value.X.(*ast.Ident)
	return ok && ident.Name == "desc" && value.Sel.Name == "Name"
}

func checkMCPToolDescriptorName(t *testing.T, fset *token.FileSet, file string, lit *ast.CompositeLit, memoryAdapterAliases map[string]struct{}) {
	t.Helper()

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			continue
		}
		if err := checkMCPToolDescriptorNameValue(kv.Value, memoryAdapterAliases); err != nil {
			t.Fatalf("%s:%d registers mcp.ToolDescriptor with invalid Name: %v", file, fset.Position(kv.Value.Pos()).Line, err)
		}
	}
}

func checkMCPToolDescriptorNameValue(expr ast.Expr, memoryAdapterAliases map[string]struct{}) error {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			raw, _ := strconv.Unquote(value.Value)
			return &toolNameError{"string literal " + strconv.Quote(raw) + "; use the memory adapters ToolSearchMemory constant"}
		}
	case *ast.SelectorExpr:
		pkg, ok := value.X.(*ast.Ident)
		if ok && value.Sel.Name == "ToolSearchMemory" {
			if _, ok := memoryAdapterAliases[pkg.Name]; ok {
				return nil
			}
		}
		return &toolNameError{"selector expression is not an allowed memory tool constant"}
	}
	return &toolNameError{"use the memory adapters ToolSearchMemory constant"}
}

func isSDKNewToolCall(call *ast.CallExpr, sdkAliases map[string]struct{}) bool {
	return isSDKNewToolFun(call.Fun, sdkAliases)
}

func isSDKNewToolFun(expr ast.Expr, sdkAliases map[string]struct{}) bool {
	switch value := expr.(type) {
	case *ast.SelectorExpr:
		return selectorIsSDKNewTool(value, sdkAliases)
	case *ast.IndexExpr:
		return isSDKNewToolFun(value.X, sdkAliases)
	case *ast.IndexListExpr:
		return isSDKNewToolFun(value.X, sdkAliases)
	default:
		return false
	}
}

func selectorIsSDKNewTool(sel *ast.SelectorExpr, sdkAliases map[string]struct{}) bool {
	if sel.Sel.Name != "NewTool" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = sdkAliases[pkg.Name]
	return ok
}

func checkSDKNewToolNameValue(call *ast.CallExpr, constants map[string]struct{}) error {
	if len(call.Args) == 0 {
		return &toolNameError{"sdk.NewTool missing name argument"}
	}
	return checkSDKToolNameValue(call.Args[0], constants, false)
}

func TestBuiltInToolNamesRejectRawStringInSDKNewTool(t *testing.T) {
	t.Parallel()

	expr, err := parser.ParseExpr(`sdk.NewTool[map[string]any]("raw_tool", "desc", fn)`)
	if err != nil {
		t.Fatalf("parse expression: %v", err)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected call expression")
	}
	if err := checkSDKNewToolNameValue(call, nil); err == nil {
		t.Fatal("sdk.NewTool with a raw string name must be rejected")
	}

	expr, err = parser.ParseExpr(`sdk.NewTool[map[string]any](ToolRead.String(), "desc", fn)`)
	if err != nil {
		t.Fatalf("parse expression: %v", err)
	}
	call, ok = expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected call expression")
	}
	if err := checkSDKNewToolNameValue(call, map[string]struct{}{"ToolRead": {}}); err != nil {
		t.Fatalf("sdk.NewTool with ToolName.String() should pass: %v", err)
	}
}

func TestBuiltInToolNamesRejectRawStringInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		`available.Has("send")`,
		`available.Ref("send")`,
		`available.Ref(("send"))`,
		`available.Refs("send", ToolReact)`,
		`available.Ref(ToolName("send"))`,
		`available.Ref((ToolName("send")))`,
		`available.Ref(ToolName(("send")))`,
		`available.Ref(tools.ToolName("send"))`,
		`available.Ref(tools.ToolName(("send")))`,
		`available.Refs([]ToolName{"send"}...)`,
		`available.Refs([]tools.ToolName{"send"}...)`,
		`available.Refs([]ToolName{"future_tool"}...)`,
		`available.Refs([]tools.ToolName{"future_tool"}...)`,
	} {
		parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", []byte("package tools\nfunc usage(available AvailableTools) { "+src+" }"), 0)
		if err != nil {
			t.Fatalf("parse source %q: %v", src, err)
		}
		availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
		var rejected bool
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, map[string]struct{}{"tools": {}}, nil, true); ok && (raw == "send" || raw == "future_tool") {
				rejected = true
			}
			return true
		})
		if !rejected {
			t.Fatalf("expected %q to be rejected as a raw available tool reference", src)
		}
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", []byte("package tools\nfunc usage(available AvailableTools) { available.Ref(ToolSend) }"), 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, nil, true); ok {
			t.Fatalf("available.Ref(ToolSend) should pass, rejected raw=%q", raw)
		}
		return true
	})
}

func TestBuiltInToolNamesRejectRawConstInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	const rawUntyped = "send"
	const rawTyped ToolName = "react"
	var rawVar = "search_memory"
	rawShort := "web_fetch"
	rawFuture := "future_tool"
	const (
		rawBlock ToolName = "speak"
		rawBlockAlias
	)
	available.Ref(rawUntyped)
	available.Ref(rawTyped)
	available.Ref(ToolName(rawUntyped))
	available.Ref(ToolName((rawUntyped)))
	available.Ref(ToolName(rawVar))
	available.Ref(ToolName((rawVar)))
	available.Ref(ToolName(rawShort))
	available.Ref(ToolName(rawFuture))
	available.Ref(rawBlockAlias)
	available.Ref(ToolSend)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawConstNames := rawToolNameNameObjects(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, nil, nil, true); ok {
			switch raw {
			case "send", "react", "speak", "search_memory", "web_fetch", "future_tool":
				rejected++
			default:
				t.Fatalf("unexpected rejected raw const %q", raw)
			}
		}
		return true
	})
	if rejected != 9 {
		t.Fatalf("expected nine raw const/var AvailableTools refs rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectShadowedToolPrefixConstInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	const ToolSend ToolName = "future_tool"
	available.Ref(ToolSend)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawConstNames := rawToolNameConstObjects("tools.go", parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, nil, nil, true); ok {
			if raw != "future_tool" {
				t.Fatalf("unexpected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected shadowed Tool* const ref rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectTypedToolNameAssignmentInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	var name ToolName
	name = "future_tool"
	available.Ref(name)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawNames := rawToolNameNameObjects(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawNames, nil, nil, nil, true); ok {
			if raw != "future_tool" {
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected one typed ToolName assignment rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesAllowsTypedToolNameReassignedToConstant(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	var name ToolName
	name = "future_tool"
	name = ToolSend
	available.Ref(name)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawNames := rawToolNameNameObjects(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, rawNames, nil, nil, nil, true); ok {
			rejected++
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected reassigned ToolName constant not to be rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesAllowsTypedToolNameShortReassignedToConstant(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	var name ToolName
	name = "future_tool"
	name, other := ToolSend, 1
	_ = other
	available.Ref(name)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawState := newRawToolNameState(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		rawState.update(n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, rawState.names, rawState.sliceNames, nil, nil, true); ok {
			rejected++
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected short reassigned ToolName constant not to be rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectsRawToolNameBeforeReassignment(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	var name ToolName
	name = "future_tool"
	available.Ref(name)
	name = ToolSend
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawState := newRawToolNameState(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		rawState.update(n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawState.names, rawState.sliceNames, nil, nil, true); ok {
			if raw != "future_tool" {
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected raw ToolName use before reassignment rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectRawSliceVarInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	rawNames := []ToolName{"future_tool"}
	rawMore := []ToolName{ToolName("send")}
	cleanNames := []ToolName{ToolSend}
	reassignedNames := []ToolName{ToolSend}
	reassignedNames = append(reassignedNames, "future_tool")
	available.Refs(rawNames...)
	available.Refs(rawNames[:]...)
	available.Refs(rawMore...)
	available.Refs(append([]ToolName{}, "future_tool")...)
	available.Refs(append([]ToolName{"future_tool"}, ToolSend)...)
	available.Refs(append(rawNames, ToolSend)...)
	available.Refs(append(cleanNames, "future_tool")...)
	available.Refs(append(cleanNames, []ToolName{"future_tool"}...)...)
	available.Refs(reassignedNames...)
	available.Refs([]ToolName{ToolSend}...)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawNames := rawToolNameNameObjects(parsed, nil, true)
	toolNameSliceNames := toolNameSliceNameObjects(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawNames, toolNameSliceNames, nil, nil, true); ok {
			switch raw {
			case "future_tool", "send":
				rejected++
			default:
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
		}
		return true
	})
	if rejected != 9 {
		t.Fatalf("expected nine raw slice refs rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectsRawSliceBeforeReassignment(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage(available AvailableTools) {
	names := []ToolName{"future_tool"}
	available.Refs(names...)
	names = []ToolName{ToolSend}
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawState := newRawToolNameState(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		rawState.update(n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawState.names, rawState.sliceNames, nil, nil, true); ok {
			if raw != "future_tool" {
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected raw slice use before reassignment rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectExternalSelectorConstInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

import "github.com/memohai/memoh/internal/userinput"

func usage(available AvailableTools) {
	available.Ref(userinput.ToolNameAskUser)
	const raw = userinput.ToolNameAskUser
	available.Ref(raw)
	available.Ref(ToolAskUser)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawConstNames := rawToolNameConstObjects("tools.go", parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, nil, nil, true); ok {
			switch raw {
			case "userinput.ToolNameAskUser":
				rejected++
			default:
				t.Fatalf("unexpected rejected selector %q", raw)
			}
		}
		return true
	})
	if rejected != 2 {
		t.Fatalf("expected two external selector AvailableTools refs rejected, got %d", rejected)
	}
}

func TestBuiltInToolNamesRejectShadowedImportAliasSelectorInAvailableToolsRefs(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import agenttools "github.com/memohai/memoh/internal/agent/tools"

type shadow struct{ ToolAskUser agenttools.ToolName }

func usage(available agenttools.AvailableTools) {
	agenttools := shadow{ToolAskUser: agenttools.ToolName("future_tool")}
	available.Ref(agenttools.ToolAskUser)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	rawConstNames := rawToolNameConstObjects("agent.go", parsed, aliases, false)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, aliases, nil, false); ok {
			if raw != "agenttools.ToolAskUser" {
				t.Fatalf("unexpected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected shadowed import alias selector ref rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardFindsLocalFactoryResults(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func fromLoaded() AvailableTools {
	return NewAvailableTools(nil)
}

func fromLoadedWithError() (AvailableTools, error) {
	return NewAvailableTools(nil), nil
}

func fromErrorThenLoaded() (error, AvailableTools) {
	return nil, NewAvailableTools(nil)
}

type providerFactory struct{}

func (providerFactory) available() AvailableTools {
	return NewAvailableTools(nil)
}

func usage() {
	var factory providerFactory
	available := fromLoaded()
	loaded, _ := fromLoadedWithError()
	_, later := fromErrorThenLoaded()
	methodAvailable := factory.available()
	current := available
	available.Ref("send")
	current.Ref("react")
	loaded.Ref("read_file")
	later.Ref("write_file")
	methodAvailable.Ref("append_file")
	factory.available().Ref("web_fetch")
	_ = current.names
	_ = methodAvailable.names
	_ = factory.available().names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	rawConstNames := rawToolNameConstObjects("tools.go", parsed, nil, true)
	if len(availableIndex.names) < 2 {
		t.Fatalf("AvailableTools factory result/alias was not recognized: %v", availableIndex)
	}
	var rawRef, backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, nil, factoryNames, true); ok && (raw == "send" || raw == "react" || raw == "read_file" || raw == "write_file" || raw == "append_file" || raw == "web_fetch") {
				rawRef++
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, factoryNames, true) {
				backingAccess++
			}
		}
		return true
	})
	if rawRef != 6 || backingAccess != 3 {
		t.Fatalf("factory-result guard rawRef=%d backingAccess=%d, want 6/3", rawRef, backingAccess)
	}
}

func TestAvailableToolsGuardFindsCompositeLiteralMethodFactoryResults(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type providerFactory struct{}
func (providerFactory) available() AvailableTools {
	return NewAvailableTools(nil)
}

func usage() {
	providerFactory{}.available().Ref("send")
	(&providerFactory{}).available().Has("react")
	_ = providerFactory{}.available().names
	_ = (&providerFactory{}).available().names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	var rawRef, backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
				switch raw {
				case "send", "react":
					rawRef++
				default:
					t.Fatalf("unexpected raw ref %q", raw)
				}
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, factoryNames, true) {
				backingAccess++
			}
		}
		return true
	})
	if rawRef != 2 || backingAccess != 2 {
		t.Fatalf("composite factory guard rawRef=%d backingAccess=%d, want 2/2", rawRef, backingAccess)
	}
}

func TestAvailableToolsGuardRejectsRawRefsThroughCallsAndLiterals(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func fromLoaded() AvailableTools {
	return NewAvailableTools(nil)
}

func usage() {
	NewAvailableTools(nil).Ref("send")
	fromLoaded().Has("react")
	AvailableTools{}.Refs("speak")
	_ = NewAvailableTools(nil).names
	_ = fromLoaded().names
	_ = AvailableTools{}.names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	rawConstNames := rawToolNameConstObjects("tools.go", parsed, nil, true)
	var rawRef, backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, nil, factoryNames, true); ok {
				switch raw {
				case "send", "react", "speak":
					rawRef++
				default:
					t.Fatalf("unexpected raw ref %q", raw)
				}
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, factoryNames, true) {
				backingAccess++
			}
		}
		return true
	})
	if rawRef != 3 || backingAccess != 3 {
		t.Fatalf("call/literal guard rawRef=%d backingAccess=%d, want 3/3", rawRef, backingAccess)
	}
}

func TestAvailableToolsGuardFindsCrossFileFactoryNames(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage() {
	fromLoaded().Ref("send")
	_ = fromLoaded().names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := map[string]struct{}{"fromLoaded": {}}
	var rawRef, backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
				if raw != "send" {
					t.Fatalf("unexpected raw ref %q", raw)
				}
				rawRef++
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, factoryNames, true) {
				backingAccess++
			}
		}
		return true
	})
	if rawRef != 1 || backingAccess != 1 {
		t.Fatalf("cross-file factory guard rawRef=%d backingAccess=%d, want 1/1", rawRef, backingAccess)
	}
}

func TestAvailableToolsGuardFindsCrossFileMultiReturnFactoryIndexes(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage() {
	_, available := fromErrorThenLoaded()
	available.Ref("send")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	factoryNames := map[string]struct{}{"fromErrorThenLoaded": {}}
	factoryInfo := map[string]map[int]struct{}{
		"fromErrorThenLoaded": {1: {}},
	}
	availableIndex := availableToolsUsageIndexForFileWithFactories(parsed, nil, true, factoryNames, factoryInfo)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
			if raw != "send" {
				t.Fatalf("unexpected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected cross-file multi-return factory raw ref rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardFindsVarMultiReturnFactoryIndexes(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func fromErrorThenLoaded() (error, AvailableTools) {
	return nil, NewAvailableTools(nil)
}

func usage() {
	var err, available = fromErrorThenLoaded()
	_ = err
	available.Ref("send")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
			if raw != "send" {
				t.Fatalf("unexpected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected var multi-return factory raw ref rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardIgnoresUnrelatedSameNamedFactoryMethod(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type factory struct{}
func (factory) available() AvailableTools { return NewAvailableTools(nil) }

type unrelated struct{}
func (unrelated) Ref(string) bool { return true }

type otherFactory struct{}
func (otherFactory) available() unrelated { return unrelated{} }

func usage(other otherFactory) {
	other.available().Ref("send")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
			rejected++
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected unrelated same-named method not to be rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardIgnoresShadowedFactoryFunctionName(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func fromLoaded() AvailableTools {
	return NewAvailableTools(nil)
}

type unrelated struct{}
func (unrelated) Ref(string) bool { return true }

func usage() {
	fromLoaded := func() unrelated { return unrelated{} }
	fromLoaded().Ref("send")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	factoryNames := availableToolsFactoryNames(parsed, nil, true, availableToolsTypeAliases(parsed))
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, factoryNames, true); ok {
			rejected++
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected shadowed factory function name not to be rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardIgnoresImportedSelectorFactoryName(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import (
	agenttools "github.com/memohai/memoh/internal/agent/tools"
	reprofactory "github.com/memohai/memoh/internal/agent/reprofactory"
)

func MakeAvailable() agenttools.AvailableTools {
	return agenttools.NewAvailableTools(nil)
}

func usage() {
	reprofactory.MakeAvailable().Ref("not_a_memoh_tool")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	factoryNames := availableToolsFactoryNames(parsed, aliases, false, availableToolsTypeAliases(parsed))
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, aliases, factoryNames, false); ok {
			rejected++
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected imported selector factory name not to be rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardRejectsPackageRawToolNameConstant(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import agenttools "github.com/memohai/memoh/internal/agent/tools"

func usage(available agenttools.AvailableTools) {
	available.Ref(rawCrossFileToolName)
	available.Ref(agenttools.ToolName(rawCrossFileToolName))
	available.Refs([]agenttools.ToolName{rawCrossFileToolName}...)
	available.Refs(append([]agenttools.ToolName{}, rawCrossFileToolName)...)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	packageRawNames := map[string]string{"rawCrossFileToolName": "future_tool"}
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Ref" || sel.Sel.Name == "Has" || sel.Sel.Name == "Refs") {
			if !isAvailableToolsBackingReceiver(sel.X, availableIndex, aliases, nil, false) {
				return true
			}
			for _, arg := range call.Args {
				if raw, ok := packageRawNameArg(arg, packageRawNames, aliases, false); ok {
					if raw != "future_tool" {
						t.Fatalf("unexpected raw ref %q", raw)
					}
					rejected++
				}
			}
		}
		return true
	})
	if rejected != 4 {
		t.Fatalf("expected four package raw ToolName refs rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardRejectsPackageSelectorRawToolNameConstant(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import agenttools "github.com/memohai/memoh/internal/agent/tools"

func usage(available agenttools.AvailableTools) {
	available.Ref(rawCrossFileSelectorToolName)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	packageRawNames := map[string]string{"rawCrossFileSelectorToolName": "userinput.ToolNameAskUser"}
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Ref" || sel.Sel.Name == "Has" || sel.Sel.Name == "Refs") {
			if !isAvailableToolsBackingReceiver(sel.X, availableIndex, aliases, nil, false) {
				return true
			}
			for _, arg := range call.Args {
				if raw, ok := packageRawNameArg(arg, packageRawNames, aliases, false); ok {
					if raw != "userinput.ToolNameAskUser" {
						t.Fatalf("unexpected raw ref %q", raw)
					}
					rejected++
				}
			}
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected package selector raw ToolName ref rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardAllowsAvailableToolsSliceLiteral(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage() {
	items := []AvailableTools{NewAvailableTools(nil)}
	_ = items
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isDirectAvailableToolsType(lit.Type, nil, true, availableToolsTypeAliases(parsed)) {
			return true
		}
		rejected++
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected AvailableTools slice literal not to be treated as backing construction, got %d", rejected)
	}
}

func TestAvailableToolsGuardRejectsBackingAccessThroughSelectors(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type holder struct {
	available AvailableTools
	current AvailableTools
	AvailableTools
}

func usage(items []AvailableTools, h holder) {
	_ = h.available.names
	_ = h.current.names
	_ = items[0].names
	_ = h.AvailableTools.names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	var backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "names" {
			return true
		}
		if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, nil, true) {
			backingAccess++
		}
		return true
	})
	if backingAccess != 4 {
		t.Fatalf("expected four selector/index backing accesses rejected, got %d", backingAccess)
	}
}

func TestAvailableToolsGuardRejectsRawRefsThroughSelectors(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type holder struct {
	available AvailableTools
	current AvailableTools
	AvailableTools
}

func usage(items []AvailableTools, h holder) {
	const rawTyped ToolName = "react"
	h.available.Ref("send")
	h.current.Ref("read_file")
	items[0].Ref(ToolName("web_fetch"))
	h.AvailableTools.Has(rawTyped)
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	rawNames := rawToolNameNameObjects(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawNames, nil, nil, nil, true); ok {
			switch raw {
			case "send", "read_file", "web_fetch", "react":
				rejected++
			default:
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
		}
		return true
	})
	if rejected != 4 {
		t.Fatalf("expected four selector/index raw refs rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardRejectsPointerAndMapReceivers(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type LocalAvailableTools = AvailableTools

func usage(available AvailableTools, alias LocalAvailableTools, pointer *AvailableTools, byName map[string]AvailableTools) {
	pointer.Ref("send")
	byName["chat"].Ref("react")
	(&available).Ref("read_file")
	alias.Ref("append_file")
	(*pointer).Ref("web_fetch")
	new(AvailableTools).Ref("search_messages")
	(&AvailableTools{}).Ref("list_sessions")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, nil, true); ok {
			switch raw {
			case "send", "react", "read_file", "append_file", "web_fetch", "search_messages", "list_sessions":
				rejected++
			default:
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
		}
		return true
	})
	if rejected != 7 {
		t.Fatalf("expected seven pointer/map raw refs rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardFindsEmbeddedAliasReceivers(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type LocalAvailableTools = AvailableTools

type holder struct {
	LocalAvailableTools
}

func usage(h holder) {
	h.LocalAvailableTools.Ref("send")
	_ = h.LocalAvailableTools.names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	var rawRef, backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, nil, true); ok {
				if raw != "send" {
					t.Fatalf("unexpected raw ref %q", raw)
				}
				rawRef++
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, nil, true) {
				backingAccess++
			}
		}
		return true
	})
	if rawRef != 1 || backingAccess != 1 {
		t.Fatalf("embedded alias guard rawRef=%d backingAccess=%d, want 1/1", rawRef, backingAccess)
	}
}

func TestAvailableToolsGuardDoesNotRejectUntypedAvailableField(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

type unrelated struct{}
func (unrelated) Ref(string) bool { return true }

type holder struct {
	available unrelated
}

func usage(h holder) {
	h.available.Ref("send")
	_ = h.available.names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, _, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, nil, nil, true); ok {
				rejected++
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, nil, true) {
				rejected++
			}
		}
		return true
	})
	if rejected != 0 {
		t.Fatalf("expected unrelated available field not to be rejected, got %d", rejected)
	}
}

func TestAvailableToolsBackingAccessAllowedOnlyInCoreMethods(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools
type unrelated struct{ names map[string]struct{} }

func NewAvailableTools() {
	var available AvailableTools
	_ = available.names
}

func (a AvailableTools) Has(name ToolName) bool {
	_ = a.names
	return true
}

func leak(available AvailableTools) {
	_ = available.names
}

func okOther(other unrelated) {
	_ = other.names
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "types.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var allowed, rejected int
	availableIndex := availableToolsUsageIndexForFile(parsed, nil, true)
	ast.Inspect(parsed, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "names" {
			return true
		}
		if !isAvailableToolsBackingReceiver(sel.X, availableIndex, nil, nil, true) {
			return true
		}
		if allowedAvailableToolsBackingAccess("types.go", parsed, sel.Pos()) {
			allowed++
		} else {
			rejected++
		}
		return true
	})
	if allowed != 2 || rejected != 1 {
		t.Fatalf("backing access allowed=%d rejected=%d, want 2/1", allowed, rejected)
	}
}

func TestAvailableToolsGuardRejectsBackingCompositeLiteral(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func leak() AvailableTools {
	return AvailableTools{names: map[string]struct{}{"send": {}}}
}

func leakUnkeyed() AvailableTools {
	return AvailableTools{map[string]struct{}{"react": {}}}
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isAvailableToolsType(lit.Type, nil, true, availableToolsTypeAliases(parsed)) {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				rejected++
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if ok && key.Name == "names" && !allowedAvailableToolsBackingAccess("tools.go", parsed, key.Pos()) {
				rejected++
			}
		}
		return true
	})
	if rejected != 2 {
		t.Fatalf("expected two backing composite literals rejected, got %d", rejected)
	}
}

func TestAvailableToolsGuardFindsOnlyTypedReceivers(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import agenttools "github.com/memohai/memoh/internal/agent/tools"

type unrelated struct{}

func (unrelated) Has(string) bool { return true }

func rawAvailable(available agenttools.AvailableTools, other unrelated) {
	name := agenttools.ToolName("send")
	available.Ref(name)
	available.Ref(agenttools.ToolName("send"))
	_ = len(available.names)
	other.Has("send")
}
`)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	rawConstNames := rawToolNameConstObjects("agent.go", parsed, aliases, false)
	if len(availableIndex.names) == 0 {
		t.Fatalf("AvailableTools parameter was not recognized: %v", availableIndex)
	}
	var rejected int
	var rawConversions int
	var backingAccess int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok {
			if raw, ok := rawToolNameConversion(call, rawConstNames, aliases, false); ok {
				if raw != "send" {
					t.Fatalf("unexpected raw conversion %q", raw)
				}
				rawConversions++
			}
			if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, rawConstNames, nil, aliases, nil, false); ok {
				if raw != "send" {
					t.Fatalf("unexpected raw tool name %q", raw)
				}
				rejected++
			}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "names" {
			if isAvailableToolsBackingReceiver(sel.X, availableIndex, aliases, nil, false) {
				backingAccess++
			}
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected exactly one raw AvailableTools reference rejected, got %d", rejected)
	}
	if rawConversions != 2 {
		t.Fatalf("expected two raw ToolName conversions rejected, got %d", rawConversions)
	}
	if backingAccess != 1 {
		t.Fatalf("expected exactly one AvailableTools backing access rejected, got %d", backingAccess)
	}
}

func TestAvailableToolsGuardFindsCrossPackageAliasReceivers(t *testing.T) {
	t.Parallel()

	src := []byte(`package agent

import agenttools "github.com/memohai/memoh/internal/agent/tools"

type LocalAvailableTools = agenttools.AvailableTools

func usage(available LocalAvailableTools) {
	available.Ref("send")
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "agent.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	aliases := importAliases(parsed, toolsImportPath, "tools")
	availableIndex := availableToolsUsageIndexForFile(parsed, aliases, false)
	var rejected int
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, raw, ok := availableToolsStringLiteralCall(call, availableIndex, nil, nil, aliases, nil, false); ok {
			if raw != "send" {
				t.Fatalf("unexpected rejected raw ref %q", raw)
			}
			rejected++
		}
		return true
	})
	if rejected != 1 {
		t.Fatalf("expected cross-package alias raw ref rejected, got %d", rejected)
	}
}

func TestSDKToolNameRejectsLocalShadow(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

func usage() {
	ToolRead := ToolName("raw_tool")
	_ = []sdk.Tool{{Name: ToolRead.String()}}
}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "tools.go", src, 0)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	var rejected bool
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			child, ok := elt.(*ast.CompositeLit)
			if !ok {
				continue
			}
			if err := firstNameValueError(child, checkSDKToolNameValue, map[string]struct{}{"ToolRead": {}}); err != nil && strings.Contains(err.Error(), "local shadow") {
				rejected = true
			}
		}
		return true
	})
	if !rejected {
		t.Fatal("expected local shadow ToolRead.String() to be rejected")
	}
}

func TestBuiltInToolNamesGuardRecognizesImportAliases(t *testing.T) {
	t.Parallel()

	src := []byte(`package tools

import (
	twilight "github.com/memohai/twilight-ai/sdk"
	mcpgw "github.com/memohai/memoh/internal/mcp"
	memoryadapters "github.com/memohai/memoh/internal/memory/adapters"
)

var _ = []twilight.Tool{{Name: "raw_tool"}}
var _ = twilight.NewTool[map[string]any]("raw_tool", "desc", nil)
var _ = []mcpgw.ToolDescriptor{{Name: "search_memory"}, {Name: memoryadapters.ToolSearchMemory}}
`)
	parsed, err := parser.ParseFile(token.NewFileSet(), "alias.go", src, 0)
	if err != nil {
		t.Fatalf("parse alias file: %v", err)
	}
	sdkAliases := importAliases(parsed, sdkImportPath, "sdk")
	mcpAliases := importAliases(parsed, mcpImportPath, "mcp")
	memoryAdapterAliases := importAliases(parsed, memoryAdaptersImportPath, "adapters")
	if _, ok := sdkAliases["sdk"]; ok {
		t.Fatal("explicit SDK alias should not also register the default sdk name")
	}
	if _, ok := mcpAliases["mcp"]; ok {
		t.Fatal("explicit MCP alias should not also register the default mcp name")
	}
	if _, ok := memoryAdapterAliases["adapters"]; ok {
		t.Fatal("explicit memory adapters alias should not also register the default adapters name")
	}
	var sawSDKToolAlias, sawSDKNewToolAlias, sawMCPDescriptorAlias, sawMemoryAdapterAlias bool
	ast.Inspect(parsed, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok {
			if isSDKToolSlice(lit.Type, sdkAliases) {
				sawSDKToolAlias = true
				child := lit.Elts[0].(*ast.CompositeLit)
				if err := firstNameValueError(child, checkSDKToolNameValue, map[string]struct{}{"ToolRead": {}}); err == nil {
					t.Fatal("sdk.Tool alias with raw string name must be rejected")
				}
			}
			if isMCPToolDescriptorSlice(lit.Type, mcpAliases) {
				sawMCPDescriptorAlias = true
				child := lit.Elts[0].(*ast.CompositeLit)
				if err := firstMCPNameValueError(child, memoryAdapterAliases); err == nil {
					t.Fatal("mcp.ToolDescriptor alias with raw string name must be rejected")
				}
				if len(lit.Elts) > 1 {
					child = lit.Elts[1].(*ast.CompositeLit)
					if err := firstMCPNameValueError(child, memoryAdapterAliases); err != nil {
						t.Fatalf("mcp.ToolDescriptor should allow aliased ToolSearchMemory: %v", err)
					}
					sawMemoryAdapterAlias = true
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok && isSDKNewToolCall(call, sdkAliases) {
			sawSDKNewToolAlias = true
			if err := checkSDKNewToolNameValue(call, map[string]struct{}{"ToolRead": {}}); err == nil {
				t.Fatal("sdk.NewTool alias with raw string name must be rejected")
			}
		}
		return true
	})
	if !sawSDKToolAlias || !sawSDKNewToolAlias || !sawMCPDescriptorAlias || !sawMemoryAdapterAlias {
		t.Fatalf("alias guard did not observe all aliased tool declarations: sdk.Tool=%v sdk.NewTool=%v mcp.ToolDescriptor=%v memoryAdapter=%v", sawSDKToolAlias, sawSDKNewToolAlias, sawMCPDescriptorAlias, sawMemoryAdapterAlias)
	}
}

func firstNameValueError(lit *ast.CompositeLit, check func(ast.Expr, map[string]struct{}, bool) error, constants map[string]struct{}) error {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && key.Name == "Name" {
			return check(kv.Value, constants, false)
		}
	}
	return nil
}

func firstMCPNameValueError(lit *ast.CompositeLit, memoryAdapterAliases map[string]struct{}) error {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && key.Name == "Name" {
			return checkMCPToolDescriptorNameValue(kv.Value, memoryAdapterAliases)
		}
	}
	return nil
}

type toolNameError struct {
	msg string
}

func (e *toolNameError) Error() string {
	return e.msg
}
