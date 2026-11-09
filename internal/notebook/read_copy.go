package notebook

import (
	"reflect"
	"slices"
)

const readViewLimit = 8 << 20

// Strings are immutable. Copy every mutable container while sharing the text;
// the cache owns its value and every caller owns its returned metadata.
func copyReadPage(p Page) Page {
	p.Sources = slices.Clone(p.Sources)
	p.Replaces = slices.Clone(p.Replaces)
	p.Premises = slices.Clone(p.Premises)
	p.Recheck = slices.Clone(p.Recheck)
	p.Passages = slices.Clone(p.Passages)
	p.Related = slices.Clone(p.Related)
	if p.Correction != nil {
		v := *p.Correction
		p.Correction = &v
	}
	if p.Learning != nil {
		v := *p.Learning
		p.Learning = &v
		v.Connections = slices.Clone(v.Connections)
		if v.Probe != nil {
			q := *v.Probe
			v.Probe = &q
			q.Results = slices.Clone(q.Results)
			for i := range q.Results {
				q.Results[i].Sources = slices.Clone(q.Results[i].Sources)
			}
		}
		if v.Practice != nil {
			q := *v.Practice
			v.Practice = &q
			q.Steps = slices.Clone(q.Steps)
			for i := range q.Steps {
				q.Steps[i].Sources = slices.Clone(q.Steps[i].Sources)
			}
		}
	}
	return p
}

func copyReadPages(pages []Page) []Page {
	out := slices.Clone(pages)
	for i := range out {
		out[i] = copyReadPage(out[i])
	}
	return out
}

// Count the inline metadata and all referenced data conservatively; shared
// strings may be counted more than once. No serialization is retained or needed.
func readValueSize(v reflect.Value) int {
	n := int(v.Type().Size())
	switch v.Kind() {
	case reflect.String:
		n += v.Len()
	case reflect.Pointer:
		if !v.IsNil() {
			n += readValueSize(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			n += readValueSize(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			// Fields are already included in the containing struct's size.
			n += readValueSize(v.Field(i)) - int(v.Field(i).Type().Size())
		}
	case reflect.Map, reflect.Interface:
		// A future dynamic field needs an explicit ownership policy first.
		return readViewLimit + 1
	}
	return n
}
