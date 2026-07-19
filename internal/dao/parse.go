package dao

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var daoMarkerRe = regexp.MustCompile(`^//cube:dao\s+(.+)$`)
var redisDaoMarkerRe = regexp.MustCompile(`^//cube:redisdao\s+(.+)$`)

// Definitions holds all parsed DAO and nested struct definitions.
type Definitions struct {
	Daos      []DaoDef
	RedisDaos []RedisDaoDef
	Nested    []NestedDef
}

// DaoDef is a DAO struct definition.
type DaoDef struct {
	Name    string // struct name, e.g. "PlayerDao"
	Coll    string // collection name, e.g. "players"
	Db      string // logical database name, e.g. "game"
	DbScope string // global or sid
	Fields  []FieldDef
}

// RedisDaoDef is a Redis DAO struct definition.
type RedisDaoDef struct {
	Name      string
	Mode      string
	Key       string
	KeyType   string
	Prefix    string
	Version   string
	TTL       string
	RedisName string
	Fields    []FieldDef
}

// NestedDef is a nested struct definition.
type NestedDef struct {
	Name   string
	Fields []FieldDef
}

// FieldDef describes a single field.
type FieldDef struct {
	Name      string
	TypeStr   string    // raw type string, e.g. "int32", "map[int64]*EquipInfo"
	Kind      FieldKind // classified type kind
	Tag       DaoTag    // parsed dao tag
	MapKey    string    // for map types: key type
	MapVal    string    // for map types: value type
	SliceElem string    // for slice types: element type
	IsPtr     bool      // whether the value type is pointer (map val or slice elem)
}

type FieldKind int

const (
	KindBasic  FieldKind = iota // 0: int, string, bool, float, etc.
	KindSlice                   // 1: []T
	KindMap                     // 2: map[K]V
	KindStruct                  // 3: nested struct value
)

type DaoTag struct {
	Persist bool
	Sync    bool
	Skip    bool // dao:"-"
	Map     string
}

func parseDefDir(dir string) (*Definitions, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	defs := &Definitions{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "gen_") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}

		f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}

		extractDefs(fset, f, defs)
	}

	return defs, nil
}

func extractDefs(fset *token.FileSet, f *ast.File, defs *Definitions) {
	// Collect all struct definitions in the file
	allStructs := make(map[string]*ast.StructType)

	// Collect DAO markers: line → params
	type daoMarker struct {
		line   int
		params map[string]string
	}
	var markers []daoMarker
	var redisMarkers []daoMarker

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if m := daoMarkerRe.FindStringSubmatch(c.Text); m != nil {
				markers = append(markers, daoMarker{
					line:   fset.Position(c.Pos()).Line,
					params: parseKV(m[1]),
				})
			}
			if m := redisDaoMarkerRe.FindStringSubmatch(c.Text); m != nil {
				redisMarkers = append(redisMarkers, daoMarker{
					line:   fset.Position(c.Pos()).Line,
					params: parseKV(m[1]),
				})
			}
		}
	}

	// First pass: collect all struct names
	for _, decl := range f.Decls {
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
			if !ok {
				continue
			}
			allStructs[typeSpec.Name.Name] = structType
		}
	}

	// Second pass: find DAO structs by marker
	for _, decl := range f.Decls {
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
			if !ok {
				continue
			}

			structLine := fset.Position(typeSpec.Pos()).Line

			for _, m := range markers {
				if m.line != structLine-1 && m.line != structLine-2 {
					continue
				}

				fields := extractFieldDefs(structType, defs)
				dbScope := m.params["dbscope"]
				if dbScope == "" {
					dbScope = "global"
				}
				defs.Daos = append(defs.Daos, DaoDef{
					Name:    typeSpec.Name.Name,
					Coll:    m.params["coll"],
					Db:      m.params["db"],
					DbScope: dbScope,
					Fields:  fields,
				})
				break
			}
			for _, m := range redisMarkers {
				if m.line != structLine-1 && m.line != structLine-2 {
					continue
				}

				fields := extractFieldDefs(structType, defs)
				keyType := m.params["key_type"]
				if keyType == "" {
					keyType = resolveFieldPathType(structType, allStructs, m.params["key"])
				}
				mode := m.params["mode"]
				if mode == "" {
					mode = "ref-hmap"
				}
				defs.RedisDaos = append(defs.RedisDaos, RedisDaoDef{
					Name:      typeSpec.Name.Name,
					Mode:      mode,
					Key:       m.params["key"],
					KeyType:   keyType,
					Prefix:    m.params["prefix"],
					Version:   m.params["version"],
					TTL:       m.params["ttl"],
					RedisName: m.params["name"],
					Fields:    fields,
				})
				break
			}
		}
	}

	// Third pass: auto-detect nested structs from DAO field types recursively.
	nestedNames := make(map[string]bool)
	for _, nested := range defs.Nested {
		nestedNames[nested.Name] = true
	}
	var queue []string
	for _, dao := range defs.Daos {
		for _, field := range dao.Fields {
			if name := resolveNestedName(field); name != "" {
				queue = append(queue, name)
			}
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if nestedNames[name] {
			continue
		}
		st, ok := allStructs[name]
		if !ok {
			continue
		}
		nestedNames[name] = true
		nestedFields := extractFieldDefs(st, defs)
		defs.Nested = append(defs.Nested, NestedDef{
			Name:   name,
			Fields: nestedFields,
		})
		for _, field := range nestedFields {
			if next := resolveNestedName(field); next != "" && !nestedNames[next] {
				queue = append(queue, next)
			}
		}
	}
}

// resolveNestedName returns the struct type name if the field references a nested struct.
func resolveNestedName(f FieldDef) string {
	switch f.Kind {
	case KindStruct:
		return stripPtr(f.TypeStr)
	case KindMap:
		if f.IsPtr {
			return f.MapVal
		}
		if !isBasicType(f.MapVal) {
			return f.MapVal
		}
	case KindSlice:
		if f.IsPtr {
			return f.SliceElem
		}
		if !isBasicType(f.SliceElem) {
			return f.SliceElem
		}
	}
	return ""
}

func stripPtr(s string) string {
	if len(s) > 0 && s[0] == '*' {
		return s[1:]
	}
	return s
}

func extractFieldDefs(st *ast.StructType, defs *Definitions) []FieldDef {
	var fields []FieldDef

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // embedded (e.g. checkpoint.DirtyHook), skip
		}

		name := field.Names[0].Name
		// Skip unexported and internal fields
		if !ast.IsExported(name) {
			continue
		}

		typeStr := exprString(field.Type)
		tag := parseDaoTag(field.Tag)

		if tag.Skip {
			continue
		}

		fd := FieldDef{
			Name:    name,
			TypeStr: typeStr,
			Tag:     tag,
		}

		classifyField(&fd, field.Type)
		if fd.Kind == KindMap && fd.Tag.Map == "" {
			fd.Tag.Map = "small"
		}
		fields = append(fields, fd)
	}

	return fields
}

func classifyField(fd *FieldDef, expr ast.Expr) {
	switch t := expr.(type) {
	case *ast.MapType:
		fd.Kind = KindMap
		fd.MapKey = exprString(t.Key)
		val := t.Value
		if star, ok := val.(*ast.StarExpr); ok {
			fd.IsPtr = true
			fd.MapVal = exprString(star.X)
		} else {
			fd.MapVal = exprString(val)
		}
	case *ast.ArrayType:
		if t.Len == nil { // slice
			fd.Kind = KindSlice
			elem := t.Elt
			if star, ok := elem.(*ast.StarExpr); ok {
				fd.IsPtr = true
				fd.SliceElem = exprString(star.X)
			} else {
				fd.SliceElem = exprString(elem)
			}
		}
	case *ast.Ident:
		if isBasicType(t.Name) {
			fd.Kind = KindBasic
		} else {
			fd.Kind = KindStruct
		}
	case *ast.StarExpr:
		fd.IsPtr = true
		if ident, ok := t.X.(*ast.Ident); ok {
			if isBasicType(ident.Name) {
				fd.Kind = KindBasic
			} else {
				fd.Kind = KindStruct
			}
		}
	case *ast.SelectorExpr:
		fd.Kind = KindStruct
	default:
		fd.Kind = KindBasic
	}
}

func parseDaoTag(tag *ast.BasicLit) DaoTag {
	if tag == nil {
		return DaoTag{Persist: true, Sync: true} // default
	}

	raw := strings.Trim(tag.Value, "`")
	re := regexp.MustCompile(`dao:"([^"]*)"`)
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return DaoTag{Persist: true, Sync: true} // no dao tag = default
	}

	val := m[1]
	if val == "-" {
		return DaoTag{Skip: true}
	}

	parts := strings.Split(val, ",")
	dt := DaoTag{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch p {
		case "persist":
			dt.Persist = true
		case "sync":
			dt.Sync = true
		case "map=small", "map=fast", "map=sharded":
			dt.Map = strings.TrimPrefix(p, "map=")
		}
	}
	return dt
}

func parseKV(s string) map[string]string {
	params := make(map[string]string)
	for _, p := range strings.Fields(s) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	return params
}

func resolveFieldPathType(root *ast.StructType, allStructs map[string]*ast.StructType, path string) string {
	if root == nil || path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	current := root
	for i, part := range parts {
		field := findStructField(current, part)
		if field == nil {
			return ""
		}
		if i == len(parts)-1 {
			return exprString(field.Type)
		}
		nextName := stripPtr(exprString(field.Type))
		if idx := strings.LastIndex(nextName, "."); idx >= 0 {
			return ""
		}
		next, ok := allStructs[nextName]
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

func findStructField(st *ast.StructType, name string) *ast.Field {
	if st == nil {
		return nil
	}
	for _, field := range st.Fields.List {
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				return field
			}
		}
	}
	return nil
}

func isBasicType(name string) bool {
	switch name {
	case "bool", "byte", "rune",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64",
		"string":
		return true
	}
	return false
}

func isIntegerType(name string) bool {
	switch name {
	case "byte", "rune",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return true
	}
	return false
}

func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", exprString(t.Len), exprString(t.Elt))
	case *ast.BasicLit:
		return t.Value
	default:
		return "any"
	}
}
