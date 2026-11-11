package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// decodeStrictObject is the one reader of a JSON object an agent sends (the CLI's standard input and the HTTP bodies behind the MCP
// tools). Unknown keys are refused, and the refusal is an *unknownFieldError: it says which object held the key, which fields that
// object takes and where a field of that name does belong, so the agent's next call can be right. Decoding and trailing data are
// returned separately so each entry keeps its own errors. The error is worded here (jsonReadError) and nowhere else: an entry
// puts its own words in front of it and never rewords it, so every entry answers the same input with the same sentence.
func decodeStrictObject(data []byte, target any) (err error, extra bool) {
	// A byte order mark before the JSON (a file an editor saved, sent through a pipe) is not part of it: it was answered with
	// "malformed (at byte 3)", which names no cause an agent can see.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return jsonReadError(err, data, target), false
	}
	if err := notAnObject(data, target); err != nil {
		return err, false
	}
	var trailing any
	return nil, dec.Decode(&trailing) != io.EOF
}

// decodeObject reads the JSON object of an entry that passes over keys it does not know: the decision API (decide, label, tune),
// which the MCP tools shikii_decide, shikii_label and shikii_tune reach, and the entries the page calls (redo, the tune actions,
// restoring answers, wiki claims). What it cannot read is worded by the same reader as decodeStrictObject. Before, these entries
// read their bodies on their own and answered in encoding/json's words ("bad json: json: cannot unmarshal string into Go struct
// field .inputs of type map[string]string").
func decodeObject(body io.Reader, target any) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err // the transport's own failure (a body over its limit): the entry answers it
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // a byte order mark before the JSON is not part of it, as in decodeStrictObject
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(target); err != nil {
		return jsonReadError(err, data, target)
	}
	return notAnObject(data, target)
}

// notAnObject refuses null where an object goes. encoding/json reads null into any value without a word, so the entry went on
// with an empty request and answered about a field that was missing.
func notAnObject(data []byte, target any) error {
	if bytes.HasPrefix(bytes.TrimLeftFunc(data, unicode.IsSpace), []byte("null")) {
		return &fieldTypeError{Want: jsonShapeName(reflect.TypeOf(target)), Got: "null"}
	}
	return nil
}

// jsonReadError puts a failure to read an agent's JSON in the words of the JSON. Before, each entry worded it or did not: work
// said where the JSON was malformed, while reflect, wiki and the HTTP bodies passed on encoding/json's "invalid character '}'
// looking for beginning of object key string", and an input that was cut off came back as "unexpected EOF" everywhere (audit of
// 0.31.0). What comes back is one of the reader's own answers: a *malformedJSONError, a *fieldTypeError (also for no JSON at all
// and for null) or an *unknownFieldError; anything else keeps its cause behind "cannot read the JSON".
func jsonReadError(err error, data []byte, target any) error {
	var wrongType *json.UnmarshalTypeError
	var syntax *json.SyntaxError
	switch {
	case errors.As(err, &wrongType):
		return &fieldTypeError{Field: indexedJSONPath(wrongType.Field, reflect.TypeOf(target)), Want: jsonShapeName(wrongType.Type), Got: wrongType.Value}
	case errors.As(err, &syntax):
		return &malformedJSONError{Offset: syntax.Offset}
	case errors.Is(err, io.ErrUnexpectedEOF):
		// the input stops inside its value (a here-document or a pipe that was cut): the place is where the text ends
		return &malformedJSONError{Offset: int64(len(bytes.TrimRightFunc(data, unicode.IsSpace))), Ends: true}
	case errors.Is(err, io.EOF):
		// no JSON at all (an empty body; the CLI answers an empty standard input before the reader is asked)
		return &fieldTypeError{Want: jsonShapeName(reflect.TypeOf(target)), Got: "nothing"}
	}
	explained := explainUnknownField(err, data, target)
	var unknown *unknownFieldError
	switch {
	case errors.As(explained, &unknown):
		return unknown
	case strings.HasPrefix(err.Error(), "json: unknown field "): // a key the walk could not place (a type that reads its own JSON)
		return fmt.Errorf("the JSON has an unknown key: %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return fmt.Errorf("cannot read the JSON: %v", err)
}

// malformedJSONError is JSON that does not parse, with the place where the reading stopped.
type malformedJSONError struct {
	Offset int64 // how many bytes were read when the reading stopped
	Ends   bool  // the input ended there, before its value was closed
}

func (e *malformedJSONError) Error() string {
	if e.Ends {
		return fmt.Sprintf("the JSON is malformed (at byte %d: the input ends there, before the JSON is closed; send the whole object)", e.Offset)
	}
	return fmt.Sprintf("the JSON is malformed (at byte %d)", e.Offset)
}

// fieldTypeError is a value of the wrong JSON type, in the words of the JSON: encoding/json says "cannot unmarshal string into Go
// struct field workRequest.result.evidence of type []string", which names the program's types to an agent that sent JSON.
type fieldTypeError struct {
	Field string // the path of the value, e.g. result.evidence; "" is the whole input
	Want  string // what goes there
	Got   string // what was sent: string, number, object, array, bool
}

func (e *fieldTypeError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("pass the JSON as %s (got %s)", e.Want, e.Got)
	}
	return fmt.Sprintf("pass the JSON %s as %s (got %s)", e.Field, e.Want, e.Got)
}

// indexedJSONPath writes encoding/json's path of a value (notes.0.sources) the way the refusal of an unknown key writes a place
// (notes[0].sources): one way for both, so an agent reads them alike. The path is walked along the target's type, and a number is
// written as an index only where that type has an array. Everywhere else it is a key and stays one: a key 1 of an object read
// into a map was written measurements[1], as if the agent had sent an array (audit of 0.31.0).
func indexedJSONPath(path string, t reflect.Type) string {
	if path == "" {
		return ""
	}
	out := ""
	for _, part := range strings.Split(path, ".") {
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			if _, err := strconv.Atoi(part); err == nil {
				out += "[" + part + "]"
				t = t.Elem()
				continue
			}
		}
		out = joinJSONPath(out, part)
		switch {
		case t == nil:
		case t.Kind() == reflect.Map:
			t = t.Elem()
		case jsonObjectType(t) != nil:
			if f, known := jsonFieldNamed(jsonFieldsOf(jsonObjectType(t)), part); known {
				t = f.typ
			} else {
				t = nil
			}
		default:
			t = nil // the type no longer says what is here: what follows is written as keys
		}
	}
	return out
}

// jsonShapeName says what JSON a Go type is read from.
func jsonShapeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		return "an object {\u2026}"
	case reflect.Slice, reflect.Array:
		return "an array [\u2026]"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true / false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	}
	return t.String()
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
