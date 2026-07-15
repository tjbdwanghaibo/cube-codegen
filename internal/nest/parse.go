package nest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

const nestMarker = "//cube:nest"

// FuncInfo describes a handler function to generate code for.
type FuncInfo struct {
	RawName       string        // original function name
	Name          string        // cleaned name (without _cost suffix)
	Entities      []EntityParam // entity parameters
	Params        []NonEntityParam
	Ret           RetParam
	Err           ErrorRetParam
	IsCost        bool
	Rollback      string
	RemoteAccess  []RemoteAccessInfo
	SourceImports []ImportInfo // imports needed for type references
}

type RemoteAccessInfo struct {
	Alias          string
	ParamName      string
	RefExpr        string
	Mode           string
	Scope          string
	Type           string
	Required       bool
	AllowStale     bool
	MinVersion     string
	CacheTTLMillis string
	Accessor       string
}

type remoteStructFieldInfo struct {
	FieldName string
	Access    RemoteAccessInfo
}

type EntityParam struct {
	Index               int
	Type                string // e.g. "IPlayerEntity"
	Name                string
	GroupType           string // full type including [] prefix
	IsGroup             bool
	IsSpeEntityCategory bool
	EntityCategory      string // e.g. "entity.EntityCategoryPlayer"
	EntityKind          string // e.g. "entity.EntityKindPlayer"
}

type NonEntityParam struct {
	Index int
	Type  string
	Name  string
}

type RetParam struct {
	Type string
	Have bool
}

type ErrorRetParam struct {
	Have bool
}

// ParseResult holds the result of parsing a file.
type ParseResult struct {
	Funcs   []*FuncInfo
	Pkg     string
	Imports []ImportInfo // imports from source file (for type references)
}

// ImportInfo holds an import path and optional alias.
type ImportInfo struct {
	Alias string // empty if no alias
	Path  string
}

// parseFile parses a Go source file and extracts functions marked with //cube:nest.
func parseFile(path string) ([]*FuncInfo, string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	// Quick check: skip files without the marker
	if !strings.Contains(string(src), nestMarker) {
		return nil, "", nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, "", err
	}

	pkg := file.Name.Name
	remoteStructFields, packageImports, err := collectPackageRemoteStructFields(path, pkg)
	if err != nil {
		return nil, "", err
	}

	// Collect imports from source file
	var imports []ImportInfo
	for _, imp := range file.Imports {
		imports = append(imports, importInfoFromSpec(imp))
	}
	imports = mergeImports(imports, packageImports)

	var funcs []*FuncInfo
	for _, decl := range file.Decls {
		fnDecl, ok := decl.(*ast.FuncDecl)
		if !ok || fnDecl.Recv != nil {
			continue
		}
		marker := parseFuncMarkers(fnDecl)
		if !marker.HasNest {
			continue
		}

		fi := parseFuncDecl(fnDecl)
		if fi == nil {
			continue
		}
		fi.Rollback = marker.NestOptions["rollback"]
		attachRemoteStructFieldAccess(fi, remoteStructFields)
		funcs = append(funcs, fi)
	}

	// Attach source imports to FuncInfo for code gen
	if len(funcs) > 0 {
		// Determine which imports are actually used by entity/param types
		usedImports := collectUsedImports(funcs, imports)
		for _, f := range funcs {
			f.SourceImports = usedImports
		}
	}

	return funcs, pkg, nil
}

func collectPackageRemoteStructFields(path string, pkg string) (map[string][]remoteStructFieldInfo, []ImportInfo, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	ret := make(map[string][]remoteStructFieldInfo)
	var imports []ImportInfo
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_nest_gen.go") || strings.HasPrefix(name, "gen_") {
			continue
		}
		filePath := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filePath, err)
		}
		if file.Name == nil || file.Name.Name != pkg {
			continue
		}
		for _, imp := range file.Imports {
			imports = append(imports, importInfoFromSpec(imp))
		}
		fields, err := collectRemoteStructFields(file)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filePath, err)
		}
		for typeName, items := range fields {
			for _, item := range items {
				if remoteAliasExists(ret[typeName], item.Access.Alias) {
					return nil, nil, fmt.Errorf("nest: duplicate remote alias %q on %s", item.Access.Alias, typeName)
				}
				ret[typeName] = append(ret[typeName], item)
			}
		}
	}
	return ret, mergeImports(nil, imports), nil
}

func importInfoFromSpec(imp *ast.ImportSpec) ImportInfo {
	info := ImportInfo{
		Path: strings.Trim(imp.Path.Value, `"`),
	}
	if imp.Name != nil {
		info.Alias = imp.Name.Name
	}
	return info
}

func mergeImports(base []ImportInfo, extra []ImportInfo) []ImportInfo {
	seen := make(map[string]bool, len(base)+len(extra))
	ret := make([]ImportInfo, 0, len(base)+len(extra))
	for _, imp := range append(base, extra...) {
		key := imp.Alias + "\x00" + imp.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		ret = append(ret, imp)
	}
	return ret
}

func collectRemoteStructFields(file *ast.File) (map[string][]remoteStructFieldInfo, error) {
	ret := make(map[string][]remoteStructFieldInfo)
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok || structType.Fields == nil {
				continue
			}
			for _, field := range structType.Fields.List {
				if field.Tag == nil || len(field.Names) == 0 {
					continue
				}
				tagValue, ok := remoteTagValue(field.Tag.Value)
				if !ok {
					continue
				}
				fieldType := types.ExprString(field.Type)
				if fieldType != "entity.RemoteViewRef" {
					return nil, fmt.Errorf("nest: remote tag field %s.%s must be entity.RemoteViewRef, got %s", typeSpec.Name.Name, field.Names[0].Name, fieldType)
				}
				for _, name := range field.Names {
					access, err := parseRemoteTag(tagValue)
					if err != nil {
						return nil, fmt.Errorf("nest: remote tag %s.%s: %w", typeSpec.Name.Name, name.Name, err)
					}
					if access.Type == "" {
						return nil, fmt.Errorf("nest: remote tag %s.%s missing snapshot type", typeSpec.Name.Name, name.Name)
					}
					if access.Alias == "" {
						access.Alias = aliasFromRemoteRefField(name.Name)
					}
					if access.Accessor == "" {
						access.Accessor = identifierFromAlias(access.Alias)
					}
					if remoteAliasExists(ret[typeSpec.Name.Name], access.Alias) {
						return nil, fmt.Errorf("nest: duplicate remote alias %q on %s", access.Alias, typeSpec.Name.Name)
					}
					ret[typeSpec.Name.Name] = append(ret[typeSpec.Name.Name], remoteStructFieldInfo{
						FieldName: name.Name,
						Access:    access,
					})
				}
			}
		}
	}
	return ret, nil
}

func remoteAliasExists(fields []remoteStructFieldInfo, alias string) bool {
	for _, field := range fields {
		if field.Access.Alias == alias {
			return true
		}
	}
	return false
}

func remoteTagValue(raw string) (string, bool) {
	tag, err := strconv.Unquote(raw)
	if err != nil {
		return "", false
	}
	value := reflect.StructTag(tag).Get("remote")
	return value, value != ""
}

func attachRemoteStructFieldAccess(fi *FuncInfo, remoteStructFields map[string][]remoteStructFieldInfo) {
	if fi == nil || len(remoteStructFields) == 0 {
		return
	}
	for _, p := range fi.Params {
		fields := remoteStructFields[remoteStructLookupType(p.Type)]
		for _, field := range fields {
			access := field.Access
			access.ParamName = p.Name
			access.RefExpr = p.Name + "." + field.FieldName
			if access.MinVersion == "" {
				access.MinVersion = access.RefExpr + ".Version"
			}
			fi.RemoteAccess = append(fi.RemoteAccess, access)
		}
	}
}

func remoteStructLookupType(typeName string) string {
	typeName = strings.TrimPrefix(typeName, "*")
	if dot := strings.LastIndex(typeName, "."); dot >= 0 {
		return typeName[dot+1:]
	}
	return typeName
}

func parseRemoteTag(raw string) (RemoteAccessInfo, error) {
	info := RemoteAccessInfo{Mode: "cache"}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			switch strings.TrimSpace(k) {
			case "alias":
				info.Alias = strings.TrimSpace(v)
			case "accessor":
				info.Accessor = strings.TrimSpace(v)
			case "ttl_ms":
				info.CacheTTLMillis = strings.TrimSpace(v)
			case "min_version":
				info.MinVersion = strings.TrimSpace(v)
			default:
				return info, fmt.Errorf("unknown remote tag option %q", strings.TrimSpace(k))
			}
			continue
		}
		switch strings.ToLower(part) {
		case "cache":
			info.Mode = "cache"
		case "read_only", "readonly", "read":
			info.Mode = "read_only"
		case "write":
			return info, fmt.Errorf("write remote tag is not supported by snapshot access; use nest remote entity dispatch")
		case "required":
			info.Required = true
		case "allow_stale":
			info.AllowStale = true
		default:
			if !strings.Contains(part, ".") {
				return info, fmt.Errorf("unknown remote tag option %q", part)
			}
			if info.Type != "" {
				return info, fmt.Errorf("duplicate remote snapshot type %q and %q", info.Type, part)
			}
			info.Type = part
		}
	}
	return info, nil
}

func aliasFromRemoteRefField(fieldName string) string {
	base := strings.TrimSuffix(fieldName, "RemoteViewRef")
	base = strings.TrimSuffix(base, "ViewRef")
	base = strings.TrimSuffix(base, "RemoteRef")
	base = strings.TrimSuffix(base, "Ref")
	if base == "" {
		base = fieldName
	}
	return camelToSnake(base)
}

func camelToSnake(raw string) string {
	var b strings.Builder
	for i, r := range raw {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

type funcMarkerInfo struct {
	HasNest     bool
	NestOptions map[string]string
}

func parseFuncMarkers(fnDecl *ast.FuncDecl) funcMarkerInfo {
	ret := funcMarkerInfo{NestOptions: make(map[string]string)}
	if fnDecl.Doc == nil {
		return ret
	}
	for _, c := range fnDecl.Doc.List {
		text := strings.TrimSpace(c.Text)
		switch {
		case strings.HasPrefix(text, nestMarker):
			ret.HasNest = true
			ret.NestOptions = parseMarkerOptions(strings.TrimSpace(strings.TrimPrefix(text, nestMarker)))
		}
	}
	return ret
}

func parseMarkerOptions(raw string) map[string]string {
	ret := make(map[string]string)
	for _, part := range strings.Fields(raw) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			ret[part] = "true"
			continue
		}
		ret[k] = strings.Trim(v, `"`)
	}
	return ret
}

func identifierFromAlias(alias string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range alias {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			if upperNext && r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			upperNext = false
			continue
		}
		upperNext = true
	}
	return b.String()
}

// collectUsedImports finds which source imports are referenced by handler entity/param types.
func collectUsedImports(funcs []*FuncInfo, imports []ImportInfo) []ImportInfo {
	// Collect all type strings
	typeRefs := make(map[string]bool)
	for _, f := range funcs {
		for _, e := range f.Entities {
			if dot := strings.Index(e.Type, "."); dot > 0 {
				typeRefs[e.Type[:dot]] = true
			}
		}
		for _, p := range f.Params {
			if dot := strings.Index(p.Type, "."); dot > 0 {
				typeRefs[p.Type[:dot]] = true
			}
		}
		if f.Ret.Have {
			if dot := strings.Index(f.Ret.Type, "."); dot > 0 {
				typeRefs[f.Ret.Type[:dot]] = true
			}
		}
		for _, access := range f.RemoteAccess {
			if dot := strings.Index(access.Scope, "."); dot > 0 {
				typeRefs[access.Scope[:dot]] = true
			}
			if dot := strings.Index(access.Type, "."); dot > 0 {
				typeRefs[access.Type[:dot]] = true
			}
		}
	}

	var used []ImportInfo
	for _, imp := range imports {
		pkgName := imp.Alias
		if pkgName == "" {
			// Derive package name from path
			parts := strings.Split(imp.Path, "/")
			pkgName = parts[len(parts)-1]
		}
		if typeRefs[pkgName] {
			used = append(used, imp)
		}
	}
	return used
}

func parseFuncDecl(fnDecl *ast.FuncDecl) *FuncInfo {
	fi := &FuncInfo{
		RawName: fnDecl.Name.Name,
		Name:    fnDecl.Name.Name,
	}

	// Handle _cost suffix
	if strings.HasSuffix(fi.Name, "_cost") {
		fi.IsCost = true
		fi.Name = strings.TrimSuffix(fi.Name, "_cost")
	}

	fnType := fnDecl.Type

	// Parse parameters
	var hasEntity bool
	var startNonEntity bool
	for _, field := range fnType.Params.List {
		typeName := types.ExprString(field.Type)

		var isGroup bool
		if isEntityGroupType(typeName) {
			isGroup = true
			hasEntity = true
		} else if isEntityCategory(typeName) {
			hasEntity = true
		} else {
			startNonEntity = true
		}

		// Handle multiple names sharing same type
		if len(field.Names) == 0 {
			continue
		}

		for _, name := range field.Names {
			if startNonEntity && !isGroup && !isEntityCategory(typeName) {
				fi.Params = append(fi.Params, NonEntityParam{
					Index: len(fi.Params),
					Type:  typeName,
					Name:  name.Name,
				})
			} else {
				baseType := strings.TrimPrefix(typeName, "[]")
				entityCategory := getSpecialEntityCategory(baseType)
				entityKind := getSpecialEntityKind(baseType)
				param := EntityParam{
					Index:     len(fi.Entities),
					Type:      baseType,
					Name:      name.Name,
					GroupType: typeName,
					IsGroup:   isGroup,
				}
				if entityCategory != "" || entityKind != "" {
					param.IsSpeEntityCategory = true
					param.EntityCategory = entityCategory
					param.EntityKind = entityKind
				}
				fi.Entities = append(fi.Entities, param)
			}
		}
	}

	if !hasEntity {
		return nil
	}

	// Parse return values
	if fnType.Results != nil {
		for _, field := range fnType.Results.List {
			typeName := types.ExprString(field.Type)
			if typeName == "error" {
				fi.Err.Have = true
			} else {
				fi.Ret.Have = true
				fi.Ret.Type = typeName
			}
		}
	}

	return fi
}

// isEntityCategory checks if a type name represents an entity parameter.
// Convention: types containing "Entity" (interface types for entity access).
func isEntityCategory(typeName string) bool {
	// Direct entity interface patterns
	if strings.Contains(typeName, "Entity") {
		return true
	}
	return false
}

// isEntityGroupType checks if a type is a slice of entity category.
func isEntityGroupType(typeName string) bool {
	if strings.HasPrefix(typeName, "[]") {
		return isEntityCategory(strings.TrimPrefix(typeName, "[]"))
	}
	return false
}

// getSpecialEntityCategory maps entity interface types to category constants by
// convention. For example, view.IPlayerEntity maps to view.EntityCategoryPlayer.
func getSpecialEntityCategory(typeName string) string {
	pkg, name := entityConstName(typeName)
	if name == "" {
		return ""
	}
	if !entityKindConstAvailable(pkg, name) {
		return ""
	}
	switch name {
	case "Player":
		return qualifyConst(pkg, "EntityCategoryPlayer")
	case "Alliance":
		return qualifyConst(pkg, "EntityCategoryAlliance")
	default:
		return qualifyConst(pkg, "EntityCategoryOther")
	}
}

// getSpecialEntityKind maps entity interface types to concrete EntityKind
// constants by convention. For example, view.IPlayerEntity maps to
// view.EntityKindPlayer.
func getSpecialEntityKind(typeName string) string {
	pkg, name := entityConstName(typeName)
	if name == "" {
		return ""
	}
	if !entityKindConstAvailable(pkg, name) {
		return ""
	}
	return qualifyConst(pkg, "EntityKind"+name)
}

func entityKindConstAvailable(pkg string, name string) bool {
	if pkg == "" {
		return true
	}
	// The cube business view package defines concrete entity kind constants.
	// Ability-style interfaces such as view.IBattleEntity intentionally do not
	// have an EntityKindBattle constant; those handlers must rely on full ID
	// metadata at runtime instead of generated kind hints.
	if pkg != "view" {
		return true
	}
	return viewEntityKindConstExists("EntityKind" + name)
}

var viewEntityKindConstCache map[string]bool

func viewEntityKindConstExists(constName string) bool {
	if viewEntityKindConstCache == nil {
		viewEntityKindConstCache = loadViewEntityKindConsts()
	}
	return viewEntityKindConstCache[constName]
}

func loadViewEntityKindConsts() map[string]bool {
	ret := make(map[string]bool)
	dir := findViewDir()
	if dir == "" {
		return ret
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ret
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		src := string(raw)
		for _, token := range strings.FieldsFunc(src, func(r rune) bool {
			return !(r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
		}) {
			if strings.HasPrefix(token, "EntityKind") {
				ret[token] = true
			}
		}
	}
	return ret
}

func findViewDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(cwd, "game", "view")
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
		next := filepath.Dir(cwd)
		if next == cwd {
			return ""
		}
		cwd = next
	}
}

func entityConstName(typeName string) (pkg string, name string) {
	if idx := strings.LastIndex(typeName, "."); idx >= 0 {
		pkg = typeName[:idx]
		typeName = typeName[idx+1:]
	}
	if !strings.HasPrefix(typeName, "I") || !strings.HasSuffix(typeName, "Entity") {
		return "", ""
	}
	name = strings.TrimSuffix(strings.TrimPrefix(typeName, "I"), "Entity")
	if name == "" {
		return "", ""
	}
	return pkg, name
}

func qualifyConst(pkg string, name string) string {
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}
