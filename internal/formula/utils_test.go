package formula

import (
	"reflect"
	"testing"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/parser/fsx"
	"github.com/goplus/xgo/token"
)

// testStruct is a test structure with both exported and unexported fields (pointer types)
type testStruct struct {
	ExportedField   *string
	unexportedField *int
	AnotherExported *bool
}

// testStructNonPtr is a test structure with non-pointer fields
type testStructNonPtr struct {
	ExportedField   string
	unexportedField int
	AnotherExported bool
}

func TestValueOf(t *testing.T) {
	t.Run("PointerFields", func(t *testing.T) {
		// Setup test data with pointer fields
		str := "test value"
		num := 42
		flag := true

		ts := testStruct{
			ExportedField:   &str,
			unexportedField: &num,
			AnotherExported: &flag,
		}

		// Use pointer to make the value addressable for unexported fields
		elem := reflect.ValueOf(&ts).Elem()

		// Test exported pointer field - valueOf should return dereferenced value
		t.Run("ExportedField", func(t *testing.T) {
			val := valueOf(elem, "ExportedField")
			if got, ok := val.(string); !ok || got != str {
				t.Errorf("valueOf(ExportedField) = %v, want %v", val, str)
			}
		})

		// Test unexported pointer field - valueOf should return the pointer itself
		t.Run("UnexportedField", func(t *testing.T) {
			val := valueOf(elem, "unexportedField")
			if got, ok := val.(*int); !ok || *got != num {
				t.Errorf("valueOf(unexportedField) = %v, want %v", val, &num)
			}
		})

		// Test another exported pointer field
		t.Run("AnotherExported", func(t *testing.T) {
			val := valueOf(elem, "AnotherExported")
			if got, ok := val.(bool); !ok || got != flag {
				t.Errorf("valueOf(AnotherExported) = %v, want %v", val, flag)
			}
		})
	})

	t.Run("NonPointerFields", func(t *testing.T) {
		// Setup test data with non-pointer fields
		str := "test value"
		num := 42
		flag := true

		ts := testStructNonPtr{
			ExportedField:   str,
			unexportedField: num,
			AnotherExported: flag,
		}

		elem := reflect.ValueOf(&ts).Elem()

		// Test exported non-pointer field
		t.Run("ExportedField", func(t *testing.T) {
			val := valueOf(elem, "ExportedField")
			if got, ok := val.(string); !ok || got != str {
				t.Errorf("valueOf(ExportedField) = %v, want %v", val, str)
			}
		})

		// Test unexported non-pointer field
		t.Run("UnexportedField", func(t *testing.T) {
			val := valueOf(elem, "unexportedField")
			if got, ok := val.(int); !ok || got != num {
				t.Errorf("valueOf(unexportedField) = %v, want %v", val, num)
			}
		})

		// Test another exported non-pointer field
		t.Run("AnotherExported", func(t *testing.T) {
			val := valueOf(elem, "AnotherExported")
			if got, ok := val.(bool); !ok || got != flag {
				t.Errorf("valueOf(AnotherExported) = %v, want %v", val, flag)
			}
		})
	})
}

func TestSetValue(t *testing.T) {
	t.Run("PointerFields", func(t *testing.T) {
		// Setup test data with pointer fields
		str := "initial"
		num := 10
		flag := false

		ts := testStruct{
			ExportedField:   &str,
			unexportedField: &num,
			AnotherExported: &flag,
		}

		elem := reflect.ValueOf(&ts).Elem()

		// Test setting exported pointer field
		t.Run("SetExportedField", func(t *testing.T) {
			newStr := "modified"
			setValue(elem, "ExportedField", &newStr)
			if *ts.ExportedField != newStr {
				t.Errorf("after setValue, ExportedField = %v, want %v", *ts.ExportedField, newStr)
			}
		})

		// Test setting unexported pointer field
		t.Run("SetUnexportedField", func(t *testing.T) {
			newNum := 99
			setValue(elem, "unexportedField", &newNum)
			if *ts.unexportedField != newNum {
				t.Errorf("after setValue, unexportedField = %v, want %v", *ts.unexportedField, newNum)
			}
		})

		// Test setting another exported pointer field
		t.Run("SetAnotherExported", func(t *testing.T) {
			newFlag := true
			setValue(elem, "AnotherExported", &newFlag)
			if *ts.AnotherExported != newFlag {
				t.Errorf("after setValue, AnotherExported = %v, want %v", *ts.AnotherExported, newFlag)
			}
		})
	})

	t.Run("NonPointerFields", func(t *testing.T) {
		// Setup test data with non-pointer fields
		str := "initial"
		num := 10
		flag := false

		ts := testStructNonPtr{
			ExportedField:   str,
			unexportedField: num,
			AnotherExported: flag,
		}

		elem := reflect.ValueOf(&ts).Elem()

		// Test setting exported non-pointer field
		t.Run("SetExportedField", func(t *testing.T) {
			newStr := "modified"
			setValue(elem, "ExportedField", newStr)
			if ts.ExportedField != newStr {
				t.Errorf("after setValue, ExportedField = %v, want %v", ts.ExportedField, newStr)
			}
		})

		// Test setting unexported non-pointer field
		t.Run("SetUnexportedField", func(t *testing.T) {
			newNum := 99
			setValue(elem, "unexportedField", newNum)
			if ts.unexportedField != newNum {
				t.Errorf("after setValue, unexportedField = %v, want %v", ts.unexportedField, newNum)
			}
		})

		// Test setting another exported non-pointer field
		t.Run("SetAnotherExported", func(t *testing.T) {
			newFlag := true
			setValue(elem, "AnotherExported", newFlag)
			if ts.AnotherExported != newFlag {
				t.Errorf("after setValue, AnotherExported = %v, want %v", ts.AnotherExported, newFlag)
			}
		})
	})
}

func TestUnexportValueOf(t *testing.T) {
	// Setup test data
	num := 123
	ts := testStruct{
		unexportedField: &num,
	}

	// Use pointer to make the value addressable
	elem := reflect.ValueOf(&ts).Elem()
	field := elem.FieldByName("unexportedField")

	// Test getting unexported field value
	unexportedVal := unexportValueOf(field)
	if !unexportedVal.CanInterface() {
		t.Error("unexportValueOf should return a value that can be interfaced")
	}

	got := unexportedVal.Interface().(*int)
	if *got != num {
		t.Errorf("unexportValueOf returned %v, want %v", *got, num)
	}
}

func TestClassfile(t *testing.T) {
	t.Run("ixgo", func(t *testing.T) {
		ctx := ixgo.NewContext(0)
		xgoContext := xgobuild.NewContext(ctx)

		_, err := xgoContext.ParseFile("testdata/formula/hello_llar.gox", nil)
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("xgo", func(t *testing.T) {
		fs := token.NewFileSet()
		_, err := parser.ParseFSEntry(fs, fsx.Local, "testdata/formula/hello_llar.gox", nil, parser.Config{
			ClassKind: xgobuild.ClassKind,
		})
		if err != nil {
			t.Error(err)
		}

	})
}
