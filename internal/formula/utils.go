package formula

import (
	"go/ast"
	"reflect"
	"unsafe"
)

// unexportValueOf creates a reflect.Value that allows access to unexported fields.
// It uses unsafe operations to bypass Go's exported field restrictions.
func unexportValueOf(field reflect.Value) reflect.Value {
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

// valueOf retrieves the value of a field by name from a struct element.
// It handles both exported and unexported fields, and both pointer and non-pointer types.
// For pointer fields, it returns the dereferenced value; for non-pointer fields, it returns the value directly.
func valueOf(elem reflect.Value, name string) any {
	if ast.IsExported(name) {
		field := elem.FieldByName(name)
		if field.Kind() == reflect.Ptr {
			return field.Elem().Interface()
		}
		return field.Interface()
	}
	return unexportValueOf(elem.FieldByName(name)).Interface()
}

// setValue sets the value of a field by name in a struct element.
// It handles both exported and unexported fields, and nil values.
// For nil values, it creates a zero value of the field's type.
func setValue(elem reflect.Value, name string, value any) {
	var val reflect.Value
	if value == nil {
		// For nil values, we need to create a zero value of the field's type
		field := elem.FieldByName(name)
		if ast.IsExported(name) {
			val = reflect.Zero(field.Type())
		} else {
			field = unexportValueOf(field)
			val = reflect.Zero(field.Type())
		}
	} else {
		val = reflect.ValueOf(value)
	}

	if ast.IsExported(name) {
		elem.FieldByName(name).Set(val)
		return
	}
	unexportValueOf(elem.FieldByName(name)).Set(val)
}
