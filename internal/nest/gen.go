package nest

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"
	"text/template"
)

const generatorVersion = "v1"

type senderMode uint8

const (
	senderModeNone senderMode = iota
	senderModeAsync
	senderModeSync
)

func generate(funcs []*FuncInfo, pkg string, outFile string, force bool, senderOnly bool, registerFunc string) (bool, error) {
	mode := senderModeNone
	if senderOnly {
		mode = senderModeAsync
	}
	return generateWithMode(funcs, pkg, outFile, force, mode, registerFunc)
}

func generateSyncSender(funcs []*FuncInfo, pkg string, outFile string, force bool) (bool, error) {
	return generateWithMode(funcs, pkg, outFile, force, senderModeSync, "")
}

func generateWithMode(funcs []*FuncInfo, pkg string, outFile string, force bool, mode senderMode, registerFunc string) (bool, error) {
	var buf bytes.Buffer

	tmpl, err := template.New("nest_gen").Funcs(template.FuncMap{
		"sub":                  func(a, b int) int { return a - b },
		"firstToUpper":         strFirstToUpper,
		"firstToLower":         strFirstToLower,
		"trimHandler":          trimHandlerPrefix,
		"hasGroup":             hasGroup,
		"gt":                   func(a, b int) bool { return a > b },
		"joinEntityIds":        joinEntityIds,
		"extraImports":         extraImports,
		"quote":                func(s string) string { return fmt.Sprintf("%q", s) },
		"rollbackMeta":         rollbackMeta,
		"remoteParamAccessors": remoteParamAccessors,
		"remoteKeyName":        remoteKeyName,
		"remoteModeExpr":       remoteModeExpr,
		"remoteScopeExpr":      remoteScopeExpr,
		"remoteTTLExpr":        remoteTTLExpr,
	}).Parse(nestTemplate)
	if err != nil {
		return false, fmt.Errorf("template parse: %w", err)
	}

	data := &templateFile{
		Package:         pkg,
		Funcs:           funcs,
		SenderOnly:      mode != senderModeNone,
		AsyncSenderOnly: mode == senderModeAsync,
		SyncSenderOnly:  mode == senderModeSync,
		HasSyncFuncs:    mode == senderModeSync && hasSyncSenderFuncs(funcs),
		RegisterFunc:    registerFunc,
	}
	if err := validateGeneratedTypeImports(funcs, data.SenderOnly, data.SyncSenderOnly); err != nil {
		return false, err
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return false, fmt.Errorf("template exec: %w", err)
	}

	content, err := format.Source(buf.Bytes())
	if err != nil {
		return false, fmt.Errorf("format generated source: %w", err)
	}

	// Check if content changed
	if !force {
		existing, err := os.ReadFile(outFile)
		if err == nil {
			existingHash := fmt.Sprintf("%x", md5.Sum(existing))
			newHash := fmt.Sprintf("%x", md5.Sum(content))
			if existingHash == newHash {
				return false, nil
			}
		}
	}

	if err := os.WriteFile(outFile, content, 0644); err != nil {
		return false, err
	}

	return true, nil
}

type templateFile struct {
	Package         string
	Funcs           []*FuncInfo
	SenderOnly      bool
	AsyncSenderOnly bool
	SyncSenderOnly  bool
	HasSyncFuncs    bool
	RegisterFunc    string
}

func hasSyncSenderFuncs(funcs []*FuncInfo) bool {
	for _, fn := range funcs {
		if fn != nil && (fn.Ret.Have || fn.Err.Have) {
			return true
		}
	}
	return false
}

func strFirstToUpper(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func strFirstToLower(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func trimHandlerPrefix(s string) string {
	s = strings.TrimPrefix(s, "handler")
	return strFirstToUpper(s)
}

func hasGroup(entities []EntityParam) bool {
	for _, e := range entities {
		if e.IsGroup {
			return true
		}
	}
	return false
}

func joinEntityIds(entities []EntityParam) string {
	var parts []string
	for _, e := range entities {
		if e.IsGroup {
			parts = append(parts, e.Name)
		} else {
			parts = append(parts, "[]int64{"+e.Name+"}")
		}
	}
	return strings.Join(parts, ", ")
}

func rollbackMeta(f *FuncInfo) string {
	switch f.Rollback {
	case "dirty":
		return "nest.HandlerMeta{Rollback: nest.RollbackDirty}"
	case "state":
		return "nest.HandlerMeta{Rollback: nest.RollbackState}"
	default:
		return "nest.HandlerMeta{}"
	}
}

type remoteParamTemplate struct {
	ParamName string
	ParamType string
	Accesses  []RemoteAccessInfo
}

func remoteParamAccessors(funcs []*FuncInfo) []remoteParamTemplate {
	byType := make(map[string]*remoteParamTemplate)
	var order []string
	for _, f := range funcs {
		for _, access := range f.RemoteAccess {
			paramType := remoteParamType(f, access.ParamName)
			if paramType == "" {
				continue
			}
			item := byType[paramType]
			if item == nil {
				item = &remoteParamTemplate{
					ParamName: access.ParamName,
					ParamType: paramType,
				}
				byType[paramType] = item
				order = append(order, paramType)
			}
			if !remoteAccessExists(item.Accesses, access.Alias) {
				item.Accesses = append(item.Accesses, access)
			}
		}
	}
	ret := make([]remoteParamTemplate, 0, len(order))
	for _, typ := range order {
		ret = append(ret, *byType[typ])
	}
	return ret
}

func remoteParamType(f *FuncInfo, paramName string) string {
	for _, p := range f.Params {
		if p.Name == paramName {
			return p.Type
		}
	}
	return ""
}

func remoteAccessExists(accesses []RemoteAccessInfo, alias string) bool {
	for _, access := range accesses {
		if access.Alias == alias {
			return true
		}
	}
	return false
}

func remoteKeyName(paramType string, accessor string) string {
	return "remoteKey" + exportedIdentifier(paramType) + exportedIdentifier(accessor)
}

func exportedIdentifier(raw string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range raw {
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

func remoteModeExpr(mode string) string {
	switch strings.ToLower(mode) {
	case "write":
		return "nest.RemoteAcquireWrite"
	case "read_only", "readonly", "read":
		return "nest.RemoteAcquireReadOnly"
	default:
		return "nest.RemoteAcquireCache"
	}
}

func remoteScopeExpr(access RemoteAccessInfo) string {
	if access.Scope != "" {
		return access.Scope
	}
	if access.Type != "" {
		return "nest.RemoteScopeOf[" + access.Type + "]()"
	}
	return "0"
}

func remoteTTLExpr(access RemoteAccessInfo) string {
	if access.CacheTTLMillis != "" {
		return access.CacheTTLMillis
	}
	if strings.EqualFold(access.Mode, "read_only") || strings.EqualFold(access.Mode, "readonly") || strings.EqualFold(access.Mode, "read") {
		return "0"
	}
	if access.Type != "" {
		return "nest.RemoteDefaultTTLMillisOf[" + access.Type + "]()"
	}
	return "0"
}

// extraImports collects imports referenced by generated type signatures.
func extraImports(funcs []*FuncInfo, senderOnly bool, syncSenderOnly bool) []ImportInfo {
	typeRefs := generatedTypeRefs(funcs, senderOnly, syncSenderOnly)

	seen := make(map[string]bool)
	resolvedTypeRefs := make(map[string]bool)
	var result []ImportInfo
	for _, f := range funcs {
		for _, imp := range f.SourceImports {
			if seen[imp.Path] {
				continue
			}
			pkgName := imp.Alias
			if pkgName == "" {
				parts := strings.Split(imp.Path, "/")
				pkgName = parts[len(parts)-1]
			}
			if typeRefs[pkgName] {
				resolvedTypeRefs[pkgName] = true
				seen[imp.Path] = true
				result = append(result, imp)
			}
		}
	}
	var unresolved []string
	for pkgName := range typeRefs {
		if !resolvedTypeRefs[pkgName] {
			unresolved = append(unresolved, pkgName)
		}
	}
	sort.Strings(unresolved)
	return result
}

func validateGeneratedTypeImports(funcs []*FuncInfo, senderOnly bool, syncSenderOnly bool) error {
	typeRefs := generatedTypeRefs(funcs, senderOnly, syncSenderOnly)
	if len(typeRefs) == 0 {
		return nil
	}
	resolved := make(map[string]bool)
	for _, f := range funcs {
		for _, imp := range f.SourceImports {
			pkgName := imp.Alias
			if pkgName == "" {
				parts := strings.Split(imp.Path, "/")
				pkgName = parts[len(parts)-1]
			}
			resolved[pkgName] = true
		}
	}
	var unresolved []string
	for pkgName := range typeRefs {
		if !resolved[pkgName] {
			unresolved = append(unresolved, pkgName)
		}
	}
	sort.Strings(unresolved)
	if len(unresolved) > 0 {
		return fmt.Errorf("nest: unresolved generated type package alias %q; import it in the handler package", unresolved[0])
	}
	return nil
}

func generatedTypeRefs(funcs []*FuncInfo, senderOnly bool, syncSenderOnly bool) map[string]bool {
	typeRefs := make(map[string]bool)
	track := func(typeName string) {
		for _, pkgName := range packageRefs(typeName) {
			typeRefs[pkgName] = true
		}
	}
	for _, f := range funcs {
		if syncSenderOnly && !f.Ret.Have && !f.Err.Have {
			continue
		}
		if !senderOnly {
			for _, e := range f.Entities {
				track(e.Type)
			}
		}
		for _, p := range f.Params {
			track(p.Type)
		}
		if f.Ret.Have && (!senderOnly || syncSenderOnly) {
			track(f.Ret.Type)
		}
		if !senderOnly {
			for _, access := range f.RemoteAccess {
				track(access.Scope)
				track(access.Type)
			}
		}
	}
	return typeRefs
}

func packageRefs(typeExpr string) []string {
	if typeExpr == "" {
		return nil
	}
	seen := make(map[string]bool)
	var refs []string
	for _, token := range strings.FieldsFunc(typeExpr, func(r rune) bool {
		return !(r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) {
		dot := strings.Index(token, ".")
		if dot <= 0 {
			continue
		}
		pkgName := token[:dot]
		if !isGoIdentifier(pkgName) || seen[pkgName] {
			continue
		}
		seen[pkgName] = true
		refs = append(refs, pkgName)
	}
	sort.Strings(refs)
	return refs
}

func isGoIdentifier(raw string) bool {
	if raw == "" {
		return false
	}
	for i, r := range raw {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

var nestTemplate = `// Code generated by tool/nest. DO NOT EDIT.
package {{.Package}}

import (
{{- if not .SenderOnly}}
	"github.com/tjbdwanghaibo/cube-core/entity"
	"errors"
	"sync"
	{{- end}}
	{{- if $.HasSyncFuncs}}
		"context"
		fctx "github.com/tjbdwanghaibo/cube-core/ctx"
	{{- end}}
		"github.com/tjbdwanghaibo/cube-core/nest"
	{{- if .AsyncSenderOnly}}
		"time"
	{{- end}}
	{{- range extraImports .Funcs .SenderOnly .SyncSenderOnly}}
		{{if .Alias}}{{.Alias}} {{end}}"{{.Path}}"
	{{- end}}
)

var (
	{{- if .AsyncSenderOnly}}
		_ = time.Second
	{{- end}}
	{{- if not .SenderOnly}}
		_ = errors.New
		_ entity.IThreadSafeEntity
{{- end}}
)
var (
{{- range .Funcs}}
	handlerName{{trimHandler .Name}} = nest.NewHandlerName("{{.RawName}}")
{{- end}}
)
{{if not .SenderOnly}}
var {{firstToLower .RegisterFunc}}Once sync.Once
{{end}}
{{range .Funcs}}
{{if not $.SenderOnly}}
func invoke{{trimHandler .Name}}(es []entity.IThreadSafeEntity, params []any, opts ...nest.HandlerOption) (ret any, err error) {
{{- $rawName := .RawName}}
{{- $handlerName := trimHandler .Name}}
{{- if hasGroup .Entities}}
	optParams := &nest.HandlerOptionParam{}
	for _, opt := range opts {
		opt(optParams)
	}
	if !optParams.IsGroup {
		err = errors.New("nest: expected group dispatch")
		return
	}

	checkELen := 0
	for _, l := range optParams.GroupLen {
		checkELen += l
	}
	if len(es) != checkELen {
		return
	}

	index := 0
{{- range $i, $p := .Entities}}
	gL{{$p.Index}} := optParams.GroupLen[{{$p.Index}}]
{{- if $p.IsGroup}}
	e{{$p.Index}} := make([]{{$p.Type}}, gL{{$p.Index}})
	for i := 0; i < gL{{$p.Index}}; i++ {
		gei, ok := es[i+index].({{$p.Type}})
		if !ok {
			err = nest.ErrEntityTypeMismatch
			return
		}
		e{{$p.Index}}[i] = gei
	}
{{- else}}
	e{{$p.Index}}, ok := es[index].({{$p.Type}})
	if !ok {
		err = nest.ErrEntityTypeMismatch
		return
	}
{{- end}}
	index += gL{{$p.Index}}
{{- end}}
{{- else}}
	if len(es) != {{len .Entities}} {
		return
	}
{{- range $i, $p := .Entities}}
	e{{$p.Index}}, ok := es[{{$i}}].({{$p.Type}})
	if !ok {
		err = nest.ErrEntityTypeMismatch
		return
	}
{{- end}}
{{- end}}

{{- if .Params}}
	if len(params) != {{len .Params}} {
		err = nest.NewParamCountMismatchError(handlerName{{trimHandler .Name}}.String(), len(params), {{len .Params}})
		return
	}
{{- range $i, $p := .Params}}
	p{{$p.Index}}, ok := params[{{$i}}].({{$p.Type}})
	if !ok {
		err = nest.NewParamTypeMismatchError(handlerName{{$handlerName}}.String(), {{$i}}, {{quote $p.Type}}, params[{{$i}}])
		return
	}
{{- end}}
{{- end}}

{{- if and .Ret.Have .Err.Have}}
	ret, err = {{.RawName}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}e{{$p.Index}}{{end}}{{if .Entities}}{{if .Params}}, {{end}}{{end}}{{range $i, $p := .Params}}{{if $i}}, {{end}}p{{$p.Index}}{{end}})
{{- else if .Ret.Have}}
	ret = {{.RawName}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}e{{$p.Index}}{{end}}{{if .Entities}}{{if .Params}}, {{end}}{{end}}{{range $i, $p := .Params}}{{if $i}}, {{end}}p{{$p.Index}}{{end}})
{{- else if .Err.Have}}
	err = {{.RawName}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}e{{$p.Index}}{{end}}{{if .Entities}}{{if .Params}}, {{end}}{{end}}{{range $i, $p := .Params}}{{if $i}}, {{end}}p{{$p.Index}}{{end}})
{{- else}}
	{{.RawName}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}e{{$p.Index}}{{end}}{{if .Entities}}{{if .Params}}, {{end}}{{end}}{{range $i, $p := .Params}}{{if $i}}, {{end}}p{{$p.Index}}{{end}})
{{- end}}
	return
}
{{end}}
	{{if $.SenderOnly}}
	{{- /* Single entity: Broadcast, Delay, Send, Sync */}}
	{{- if eq (len .Entities) 1}}{{if not (index .Entities 0).IsGroup}}
	{{if $.AsyncSenderOnly}}
	func Broadcast_{{trimHandler .Name}}(ids []int64{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
		nest.Nest.Broadcast(handlerName{{trimHandler .Name}}, ids, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}){{if .IsCost}}, nest.SendOptionIsCost(){{end}})
	}

func Delay_{{trimHandler .Name}}(delay time.Duration, id int64{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
	nest.Nest.Send(handlerName{{trimHandler .Name}}, id, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), nest.SendOptionWithDelay(delay){{if .IsCost}}, nest.SendOptionIsCost(){{end}})
}

	func Send_{{trimHandler .Name}}(id int64{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
		nest.Nest.Send(handlerName{{trimHandler .Name}}, id, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}){{if .IsCost}}, nest.SendOptionIsCost(){{end}})
	}
	{{end}}
	{{if and $.SyncSenderOnly (or .Ret.Have .Err.Have)}}
	func Sync_{{trimHandler .Name}}(ctx context.Context, id int64{{range .Params}}, {{.Name}} {{.Type}}{{end}}) ({{if .Ret.Have}}ret {{.Ret.Type}}, {{end}}err error) {
	release := fctx.BindBase(ctx)
	defer release()
	{{- if .Ret.Have}}
		retXXX, errXXX := nest.Nest.Sync(handlerName{{trimHandler .Name}}, id, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}))
	err = errXXX
	if err != nil {
		return
	}
	if retXXX == nil {
		return
	}
	ret = retXXX.({{.Ret.Type}})
{{- else}}
	_, errXXX := nest.Nest.Sync(handlerName{{trimHandler .Name}}, id, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}))
	err = errXXX
	{{- end}}
		return
	}
	{{end}}
{{- end}}{{end}}

	{{- /* Multi entity (no group): MultiDelay, MultiSend, MultiSync */}}
	{{- if and (gt (len .Entities) 1) (not (hasGroup .Entities))}}
	{{if $.AsyncSenderOnly}}
	func MultiDelay_{{trimHandler .Name}}(delay time.Duration, {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}} int64{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
		ids := []int64{ {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{end}} }
		opts := []nest.SendOpt{nest.SendOptionWithDelay(delay){{if .IsCost}}, nest.SendOptionIsCost(){{end}}}
	nest.Nest.MultiSend(handlerName{{trimHandler .Name}}, ids, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
}

func MultiSend_{{trimHandler .Name}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}} int64{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
	ids := []int64{ {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{end}} }
	opts := []nest.SendOpt{}
{{- if .IsCost}}
	opts = append(opts, nest.SendOptionIsCost())
	{{- end}}
		nest.Nest.MultiSend(handlerName{{trimHandler .Name}}, ids, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	}
	{{end}}
	{{if and $.SyncSenderOnly (or .Ret.Have .Err.Have)}}
	func MultiSync_{{trimHandler .Name}}(ctx context.Context, {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}} int64{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) ({{if .Ret.Have}}ret {{.Ret.Type}}, {{end}}err error) {
	release := fctx.BindBase(ctx)
	defer release()
		ids := []int64{ {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{end}} }
		opts := []nest.SendOpt{}
{{- if .IsCost}}
	opts = append(opts, nest.SendOptionIsCost())
{{- end}}
{{- if .Ret.Have}}
	retXXX, errXXX := nest.Nest.MultiSync(handlerName{{trimHandler .Name}}, ids, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	err = errXXX
	if err != nil {
		return
	}
	if retXXX == nil {
		return
	}
	ret = retXXX.({{.Ret.Type}})
{{- else}}
	_, errXXX := nest.Nest.MultiSync(handlerName{{trimHandler .Name}}, ids, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	err = errXXX
{{- end}}
	return
}
{{end}}
{{- end}}

	{{- /* Group entity: MultiGroupDelay, MultiGroupSend, MultiGroupSync */}}
	{{- if hasGroup .Entities}}
	{{if $.AsyncSenderOnly}}
	func MultiGroupDelay_{{trimHandler .Name}}(delay time.Duration, {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{if $p.IsGroup}} []int64{{else}} int64{{end}}{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
		groupIds := [][]int64{ {{joinEntityIds .Entities}} }
		opts := []nest.SendOpt{nest.SendOptionWithDelay(delay){{if .IsCost}}, nest.SendOptionIsCost(){{end}}}
	nest.Nest.MultiGroupSend(handlerName{{trimHandler .Name}}, groupIds, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
}

func MultiGroupSend_{{trimHandler .Name}}({{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{if $p.IsGroup}} []int64{{else}} int64{{end}}{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) {
	groupIds := [][]int64{ {{joinEntityIds .Entities}} }
	opts := []nest.SendOpt{}
{{- if .IsCost}}
	opts = append(opts, nest.SendOptionIsCost())
	{{- end}}
		nest.Nest.MultiGroupSend(handlerName{{trimHandler .Name}}, groupIds, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	}
	{{end}}
	{{if and $.SyncSenderOnly (or .Ret.Have .Err.Have)}}
	func MultiGroupSync_{{trimHandler .Name}}(ctx context.Context, {{range $i, $p := .Entities}}{{if $i}}, {{end}}{{$p.Name}}{{if $p.IsGroup}} []int64{{else}} int64{{end}}{{end}}{{range .Params}}, {{.Name}} {{.Type}}{{end}}) ({{if .Ret.Have}}ret {{.Ret.Type}}, {{end}}err error) {
	release := fctx.BindBase(ctx)
	defer release()
		groupIds := [][]int64{ {{joinEntityIds .Entities}} }
		opts := []nest.SendOpt{}
{{- if .IsCost}}
	opts = append(opts, nest.SendOptionIsCost())
{{- end}}
{{- if .Ret.Have}}
	retXXX, errXXX := nest.Nest.MultiGroupSync(handlerName{{trimHandler .Name}}, groupIds, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	err = errXXX
	if err != nil {
		return
	}
	if retXXX == nil {
		return
	}
	ret = retXXX.({{.Ret.Type}})
{{- else}}
	_, errXXX := nest.Nest.MultiGroupSync(handlerName{{trimHandler .Name}}, groupIds, nest.NewParams({{range $i, $p := .Params}}{{if $i}}, {{end}}{{$p.Name}}{{end}}), opts...)
	err = errXXX
{{- end}}
	return
}
{{end}}
{{- end}}
{{end}}
{{end}}

{{if not .SenderOnly}}
{{range remoteParamAccessors .Funcs}}
{{- $paramType := .ParamType}}
{{- $paramName := .ParamName}}
{{- range .Accesses}}
var {{remoteKeyName $paramType .Accessor}} = nest.RemoteKey[{{.Type}}]{Alias: {{quote .Alias}}}
{{- end}}

func ({{.ParamName}} {{.ParamType}}) RemoteAccess() []nest.RemoteAccess {
	return []nest.RemoteAccess{
{{- range .Accesses}}
		{
			Alias: {{quote .Alias}},
			Ref: {{.RefExpr}},
			Mode: {{remoteModeExpr .Mode}},
			Scope: {{remoteScopeExpr .}},
			MinVersion: {{if .MinVersion}}{{.MinVersion}}{{else}}0{{end}},
			{{- if .AllowStale}}
			AllowStale: true,
			{{- end}}
			CacheTTLMillis: {{remoteTTLExpr .}},
			{{- if .Required}}
			Required: true,
			{{- end}}
		},
{{- end}}
	}
}
{{range .Accesses}}
func ({{$paramName}} {{$paramType}}) {{.Accessor}}() ({{.Type}}, bool) {
	return nest.Remote({{remoteKeyName $paramType .Accessor}})
}

func ({{$paramName}} {{$paramType}}) Must{{.Accessor}}() {{.Type}} {
	return nest.MustRemote({{remoteKeyName $paramType .Accessor}})
}
{{end}}
{{end}}

func {{.RegisterFunc}}() {
	{{firstToLower .RegisterFunc}}Once.Do(func() {
{{- range .Funcs}}
		nest.MustRegisterHandlerWithMeta(handlerName{{trimHandler .Name}}, invoke{{trimHandler .Name}}, {{rollbackMeta .}})
{{- end}}
	})
}
{{end}}
`
