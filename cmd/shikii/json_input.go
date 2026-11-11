package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// decodeStrictObject is the one reader of a JSON object an agent sends (the CLI's standard input and the HTTP bodies behind the MCP
// tools). Unknown keys are refused, and the refusal is an *unknownFieldError: it says which object held the key, which fields that
// object takes and where a field of that name does belong, so the agent's next call can be right. Decoding and trailing data are
// returned separately so each entry keeps its own errors.
func decodeStrictObject(data []byte, target any) (err error, extra bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return explainUnknownField(err, data, target), false
	}
	var trailing any
	return nil, dec.Decode(&trailing) != io.EOF
}

// unknownFieldError is a refused key with what the agent needs to fix it. encoding/json names only the key; the place and the
// fields come from walking the same input along the target's type, so they can never drift from what the decoder accepts.
type unknownFieldError struct {
	Field   string   // the key as it was sent
	Path    string   // the object that held it: "" is the top level, otherwise e.g. notes[0].edits[0]
	Allowed []string // the fields that object takes, in declaration order
	Homes   []string // other objects of the same request that take a field of this name, e.g. work or wiki[]
}

func (e *unknownFieldError) Error() string {
	where := "at the top level"
	if e.Path != "" {
		where = "in " + e.Path
	}
	text := fmt.Sprintf("unknown field %q %s; the fields there: %s", e.Field, where, strings.Join(e.Allowed, ", "))
	if len(e.Homes) > 0 {
		text += fmt.Sprintf("; %q is a field of %s", e.Field, strings.Join(e.Homes, ", "))
	}
	if e.Field == "project" && e.Path == "" {
		// The MCP server takes project out before the handlers read the arguments, so a project that reaches a reader came from the
		// CLI or the HTTP endpoint, which work on one project: the folder they run in.
		text += "; project is an argument of the MCP tools: here, run the command in the project's folder (for a worktree, the original project) and leave project out"
	}
	return text
}

// explainUnknownField turns encoding/json's `json: unknown field "x"` into an *unknownFieldError. Any other error, and an unknown key
// the walk cannot place (a type that reads its own JSON), is returned unchanged.
func explainUnknownField(err error, data []byte, target any) error {
	const prefix = "json: unknown field "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		return err
	}
	field, quoteErr := strconv.Unquote(strings.TrimPrefix(err.Error(), prefix))
	if quoteErr != nil {
		return err
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&value) != nil {
		return err
	}
	root := reflect.TypeOf(target)
	found := unknownFieldIn(value, root, "", field, 0)
	if found == nil {
		return err
	}
	here := indexlessPath(found.Path)
	for _, home := range fieldHomes(root, field) {
		if home != here && len(found.Homes) < 3 {
			found.Homes = append(found.Homes, home)
		}
	}
	return found
}

type jsonField struct {
	name string
	typ  reflect.Type
}

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// jsonObjectType is the struct a JSON object is decoded into, or nil when the type reads its own JSON (time.Time, json.RawMessage)
// or is not a struct.
func jsonObjectType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t.Implements(jsonUnmarshalerType) || reflect.PointerTo(t).Implements(jsonUnmarshalerType) {
		return nil
	}
	return t
}

// jsonFieldsOf lists the keys encoding/json reads into a struct, in declaration order: exported fields under their tag names, and the
// fields of embedded structs in place.
func jsonFieldsOf(t reflect.Type) []jsonField {
	var out []jsonField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			if embedded := jsonObjectType(f.Type); embedded != nil {
				out = append(out, jsonFieldsOf(embedded)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, jsonField{name: name, typ: f.Type})
	}
	return out
}

// jsonFieldNamed finds a key the way encoding/json does: the exact name first, then any case.
func jsonFieldNamed(fields []jsonField, key string) (jsonField, bool) {
	for _, f := range fields {
		if f.name == key {
			return f, true
		}
	}
	for _, f := range fields {
		if strings.EqualFold(f.name, key) {
			return f, true
		}
	}
	return jsonField{}, false
}

func joinJSONPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// unknownFieldIn walks the decoded input along the target's type and returns the first object holding the unknown key (keys are
// visited in sorted order, the object itself before the objects inside it).
func unknownFieldIn(value any, t reflect.Type, path, field string, depth int) *unknownFieldError {
	if depth > 32 {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		object := jsonObjectType(t)
		entries, ok := value.(map[string]any)
		if object == nil || !ok {
			return nil
		}
		fields := jsonFieldsOf(object)
		if _, sent := entries[field]; sent {
			if _, known := jsonFieldNamed(fields, field); !known {
				allowed := make([]string, len(fields))
				for i, f := range fields {
					allowed[i] = f.name
				}
				return &unknownFieldError{Field: field, Path: path, Allowed: allowed}
			}
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if f, known := jsonFieldNamed(fields, key); known {
				if found := unknownFieldIn(entries[key], f.typ, joinJSONPath(path, key), field, depth+1); found != nil {
					return found
				}
			}
		}
	case reflect.Slice, reflect.Array:
		list, ok := value.([]any)
		if !ok {
			return nil
		}
		for i, element := range list {
			if found := unknownFieldIn(element, t.Elem(), path+"["+strconv.Itoa(i)+"]", field, depth+1); found != nil {
				return found
			}
		}
	case reflect.Map:
		entries, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if found := unknownFieldIn(entries[key], t.Elem(), joinJSONPath(path, key), field, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

// fieldHomes lists the objects of a request type that take a field of this name, as paths without indexes ("" is the top level,
// wiki[] an element of the wiki list), nearest first.
func fieldHomes(t reflect.Type, field string) []string {
	var homes []string
	seen := map[string]bool{}
	var walk func(t reflect.Type, path string, onPath map[reflect.Type]bool, depth int)
	walk = func(t reflect.Type, path string, onPath map[reflect.Type]bool, depth int) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		switch t.Kind() {
		case reflect.Struct:
			object := jsonObjectType(t)
			if object == nil || onPath[object] || depth > 8 {
				return
			}
			onPath[object] = true
			defer delete(onPath, object)
			fields := jsonFieldsOf(object)
			for _, f := range fields {
				if f.name == field && path != "" && !seen[path] {
					seen[path] = true
					homes = append(homes, path)
				}
			}
			for _, f := range fields {
				walk(f.typ, joinJSONPath(path, f.name), onPath, depth+1)
			}
		case reflect.Slice, reflect.Array:
			walk(t.Elem(), path+"[]", onPath, depth)
		case reflect.Map:
			walk(t.Elem(), path+".*", onPath, depth)
		}
	}
	walk(t, "", map[reflect.Type]bool{}, 0)
	sort.SliceStable(homes, func(i, j int) bool { return strings.Count(homes[i], ".") < strings.Count(homes[j], ".") })
	return homes
}

// indexlessPath turns notes[0].edits[2] into notes[].edits[], the form fieldHomes uses.
func indexlessPath(path string) string {
	var out strings.Builder
	inIndex := false
	for _, r := range path {
		switch {
		case r == '[':
			inIndex = true
			out.WriteRune(r)
		case r == ']':
			inIndex = false
			out.WriteRune(r)
		case !inIndex:
			out.WriteRune(r)
		}
	}
	return out.String()
}
