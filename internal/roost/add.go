package roost

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

type AddOptions struct {
	Kind, Name, Service, Group string
	Mods                       []string
	ID                         int64
}

func Add(root string, options AddOptions) ([]string, error) {
	m, err := LoadManifest(root)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(options.Name)
	if !validName(toSnake(name)) {
		return nil, fmt.Errorf("invalid %s name %q", options.Kind, name)
	}
	if options.Kind == "service" {
		key := toSnake(name)
		if _, exists := m.Services[key]; exists {
			return nil, fmt.Errorf("service %s already exists", key)
		}
		if _, err := resolveMods(options.Mods); err != nil {
			return nil, err
		}
		m.Services[key] = ServiceSpec{Mods: uniqueSorted(options.Mods)}
		if err := saveManifest(root, m); err != nil {
			return nil, err
		}
		_, err := SyncProject(root)
		return []string{"roost.yaml", "internal/service/" + key + "/service.go"}, err
	}
	if options.Kind == "mod" {
		service := toSnake(options.Service)
		spec, ok := m.Services[service]
		if !ok {
			return nil, fmt.Errorf("unknown service %q", service)
		}
		if _, ok := modCatalog[toSnake(name)]; !ok {
			return nil, fmt.Errorf("unknown kit mod %q", name)
		}
		spec.Mods = uniqueSorted(append(spec.Mods, toSnake(name)))
		m.Services[service] = spec
		if err := saveManifest(root, m); err != nil {
			return nil, err
		}
		_, err := SyncProject(root)
		return []string{"roost.yaml"}, err
	}
	return addArtifact(root, m, options)
}

func addArtifact(root string, m Manifest, o AddOptions) ([]string, error) {
	snake := toSnake(o.Name)
	pascal := toPascal(o.Name)
	if o.ID == 0 && (o.Kind == "protocol" || o.Kind == "entity" || o.Kind == "component" || o.Kind == "errcode") {
		id, err := NextID(root, m, o.Kind, o.Group)
		if err != nil {
			return nil, err
		}
		o.ID = id
	}
	var path, body string
	switch o.Kind {
	case "protocol":
		path = "protocol/def/" + snake + ".go"
		body = fmt.Sprintf("//go:build protocoldef\n\npackage protocoldef\n\ntype %sRequest struct{}\ntype %sResponse struct {\n\tCode int32 `pb:\"1\"`\n\tReason string `pb:\"2\"`\n}\n\ntype %sProtocol interface {\n\t//cube:msg id=%d\n\t%s(%sRequest) %sResponse\n}\n", pascal, pascal, pascal, o.ID, pascal, pascal, pascal)
	case "entity":
		path = "game/entities/" + snake + "/entity.go"
		body = fmt.Sprintf("package %s\n\nimport \"github.com/tjbdwanghaibo/cube-core/entity\"\n\nconst (\n\tEntityCategory%s entity.EntityCategory = 1\n\tEntityKind%s entity.EntityKind = %d\n)\n\nvar _ = func() struct{} { entity.MustRegisterEntityKindCategory(EntityKind%s, EntityCategory%s); return struct{}{} }()\n\n//cube:entity id=%d entityKind=EntityKind%s\ntype %s struct { *entity.EntityBase }\n", snake, pascal, pascal, o.ID, pascal, pascal, o.ID, pascal, pascal)
	case "component":
		path = "game/components/" + snake + "/component.go"
		body = fmt.Sprintf("package %s\n\nimport \"github.com/tjbdwanghaibo/cube-core/entity\"\n\n//cube:component type=%d\nconst CompType%s entity.ComponentType = %d\n\ntype %s struct { entity.ComponentBase }\n", snake, o.ID, pascal, o.ID, pascal)
	case "event":
		path = "event/def/" + snake + ".go"
		body = fmt.Sprintf("package eventdef\n\ntype Event%s struct{}\n", pascal)
	case "table":
		path = "configs/schema/" + snake + ".go"
		body = fmt.Sprintf("package schema\n\n//cube:table name=%s key=ID\ntype %s struct {\n\tID int64 `csv:\"id\" json:\"id\" title:\"ID\" required:\"true\" unique:\"true\"`\n}\n", snake, pascal)
	case "dao":
		path = "db/def/" + snake + ".go"
		body = fmt.Sprintf("package dbdef\n\n//cube:dao coll=%s db=game\ntype %s struct {\n\tID int64 `bson:\"_id\"`\n}\n", snake, pascal)
	case "webroute":
		path = "service/web/" + snake + ".go"
		body = fmt.Sprintf("package web\n\nimport (\n\t\"context\"\n\t\"github.com/tjbdwanghaibo/cube-core/webroute\"\n)\n\ntype %sResponse struct{}\n\n//cube:web method=GET path=/%s body=raw\nfunc %s(context.Context, *Service, webroute.RawRequest) (%sResponse, error) { return %sResponse{}, nil }\n", pascal, snake, pascal, pascal, pascal)
	case "errcode":
		path = "internal/errors/" + snake + ".go"
		body = fmt.Sprintf("package errors\n\nimport \"github.com/tjbdwanghaibo/cube-core/errcode\"\n\nvar Err%s = errcode.Define(%d, %q, %q)\n", pascal, o.ID, snake, "TODO")
	case "module":
		path = "game/controllers/" + snake + "/controller.go"
		body = fmt.Sprintf("package %s\n\nimport \"github.com/tjbdwanghaibo/cube-core/app\"\n\ntype Controller struct{}\nfunc FromRegistry(*app.Registry) (Controller, error) { return Controller{}, nil }\n", snake)
	default:
		return nil, fmt.Errorf("unsupported artifact kind %q", o.Kind)
	}
	formatted, err := format.Source([]byte(body))
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err == nil {
		return nil, fmt.Errorf("%s already exists", path)
	}
	if err := writeAtomic(abs, formatted, 0o644); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func saveManifest(root string, m Manifest) error {
	raw, err := m.Marshal()
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(root, ManifestName), raw, 0o644)
}

func toSnake(value string) string { return strings.ToLower(strings.Join(splitWords(value), "_")) }
func toPascal(value string) string {
	parts := splitWords(value)
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, "")
}
func splitWords(value string) []string {
	value = strings.TrimSpace(value)
	var parts []string
	var current []rune
	for i, r := range []rune(value) {
		if r == '-' || r == '_' || r == ' ' {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = nil
			}
			continue
		}
		if i > 0 && r >= 'A' && r <= 'Z' && len(current) > 0 {
			parts = append(parts, string(current))
			current = nil
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}
