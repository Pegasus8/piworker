package mcpserver

import (
	"reflect"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// withEmptySlices returns a copy of v whose nil slices, at any depth of its
// structs, slices and pointers, are empty: encoded as [] rather than null.
// The output schemas declare lists as arrays (no type unions), and clients
// refuse results that do not match, so a service answering a nil list must
// not break the tool. v itself is never modified: slices and pointees are
// copied before anything under them changes (they may be the service's).
// Maps and interface values are left as they are (free-form in the
// schemas).
func withEmptySlices[T any](v T) T {
	out := reflect.New(reflect.TypeFor[T]()).Elem()
	out.Set(reflect.ValueOf(&v).Elem())
	fillSlices(out)
	return out.Interface().(T)
}

// fillSlices fixes v (settable) in place; what it holds was already copied
// by the caller.
func fillSlices(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				fillField(f)
			}
		}
	case reflect.Slice, reflect.Pointer:
		fillField(v)
	}
}

// fillField fixes one settable value: a nil slice becomes empty, a slice
// or pointer is copied before its elements are fixed.
func fillField(f reflect.Value) {
	switch f.Kind() {
	case reflect.Slice:
		if f.IsNil() {
			f.Set(reflect.MakeSlice(f.Type(), 0, 0))
			return
		}
		if !needsFill(f.Type().Elem()) {
			return
		}
		c := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
		reflect.Copy(c, f)
		for i := range c.Len() {
			fillSlices(c.Index(i))
		}
		f.Set(c)
	case reflect.Pointer:
		if f.IsNil() || !needsFill(f.Type().Elem()) {
			return
		}
		c := reflect.New(f.Type().Elem())
		c.Elem().Set(f.Elem())
		fillSlices(c.Elem())
		f.Set(c)
	case reflect.Struct:
		fillSlices(f)
	}
}

// needsFill reports whether values of t can hold a slice to fix.
func needsFill(t reflect.Type) bool {
	return needsFillSeen(t, map[reflect.Type]bool{})
}

func needsFillSeen(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Slice:
		return true
	case reflect.Pointer:
		return needsFillSeen(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && needsFillSeen(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

// structured is the result of a built-in tool answering v: v with its nil
// lists as [] (withEmptySlices), as structured content and as the JSON
// text block.
func structured[T any](v T, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return nil, err
	}
	return StructuredResult(withEmptySlices(v))
}
