package loader

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goplus/ixgo"
	"github.com/goplus/llar/pkgs/mod/module"
)

// testStruct is a test structure with both exported and unexported fields
type testStruct struct {
	ExportedField   *string
	unexportedField *int
	AnotherExported *bool
}

func TestValueOf(t *testing.T) {
	// Setup test data
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

	// Test exported field
	t.Run("ExportedField", func(t *testing.T) {
		val := valueOf(elem, "ExportedField")
		if got, ok := val.(string); !ok || got != str {
			t.Errorf("valueOf(ExportedField) = %v, want %v", val, str)
		}
	})

	// Test unexported field
	t.Run("UnexportedField", func(t *testing.T) {
		val := valueOf(elem, "unexportedField")
		if got, ok := val.(int); !ok || got != num {
			t.Errorf("valueOf(unexportedField) = %v, want %v", val, num)
		}
	})

	// Test another exported field
	t.Run("AnotherExported", func(t *testing.T) {
		val := valueOf(elem, "AnotherExported")
		if got, ok := val.(bool); !ok || got != flag {
			t.Errorf("valueOf(AnotherExported) = %v, want %v", val, flag)
		}
	})
}

func TestSetValue(t *testing.T) {
	// Setup test data
	str := "initial"
	num := 10
	flag := false

	ts := testStruct{
		ExportedField:   &str,
		unexportedField: &num,
		AnotherExported: &flag,
	}

	elem := reflect.ValueOf(&ts).Elem()

	// Test setting exported field
	t.Run("SetExportedField", func(t *testing.T) {
		newStr := "modified"
		setValue(elem, "ExportedField", &newStr)
		if *ts.ExportedField != newStr {
			t.Errorf("after setValue, ExportedField = %v, want %v", *ts.ExportedField, newStr)
		}
	})

	// Test setting unexported field
	t.Run("SetUnexportedField", func(t *testing.T) {
		newNum := 99
		setValue(elem, "unexportedField", &newNum)
		if *ts.unexportedField != newNum {
			t.Errorf("after setValue, unexportedField = %v, want %v", *ts.unexportedField, newNum)
		}
	})

	// Test setting another exported field
	t.Run("SetAnotherExported", func(t *testing.T) {
		newFlag := true
		setValue(elem, "AnotherExported", &newFlag)
		if *ts.AnotherExported != newFlag {
			t.Errorf("after setValue, AnotherExported = %v, want %v", *ts.AnotherExported, newFlag)
		}
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

func TestStructElemValueAndSetValue(t *testing.T) {
	// Setup test data
	str := "test"
	num := 42
	flag := true

	ts := testStruct{
		ExportedField:   &str,
		unexportedField: &num,
		AnotherExported: &flag,
	}

	// Use pointer to make the value addressable for unexported fields
	se := &StructElem{elem: reflect.ValueOf(&ts).Elem()}

	// Test Value method
	t.Run("Value", func(t *testing.T) {
		// Test exported field
		if got := se.Value("ExportedField").(string); got != str {
			t.Errorf("StructElem.Value(ExportedField) = %v, want %v", got, str)
		}

		// Test unexported field
		if got := se.Value("unexportedField").(int); got != num {
			t.Errorf("StructElem.Value(unexportedField) = %v, want %v", got, num)
		}

		// Test another exported field
		if got := se.Value("AnotherExported").(bool); got != flag {
			t.Errorf("StructElem.Value(AnotherExported) = %v, want %v", got, flag)
		}
	})

	// Test SetValue method
	t.Run("SetValue", func(t *testing.T) {
		// Create a new StructElem with a pointer to allow modifications
		tsPtr := &testStruct{
			ExportedField:   &str,
			unexportedField: &num,
			AnotherExported: &flag,
		}
		sePtr := &StructElem{elem: reflect.ValueOf(tsPtr).Elem()}

		// Test setting exported field
		newStr := "new value"
		sePtr.SetValue("ExportedField", &newStr)
		if *tsPtr.ExportedField != newStr {
			t.Errorf("after SetValue, ExportedField = %v, want %v", *tsPtr.ExportedField, newStr)
		}

		// Test setting unexported field
		newNum := 999
		sePtr.SetValue("unexportedField", &newNum)
		if *tsPtr.unexportedField != newNum {
			t.Errorf("after SetValue, unexportedField = %v, want %v", *tsPtr.unexportedField, newNum)
		}

		// Test setting another exported field
		newFlag := false
		sePtr.SetValue("AnotherExported", &newFlag)
		if *tsPtr.AnotherExported != newFlag {
			t.Errorf("after SetValue, AnotherExported = %v, want %v", *tsPtr.AnotherExported, newFlag)
		}
	})
}

func TestNewFormulaLoader(t *testing.T) {
	// Test that NewFormulaLoader returns a non-nil Loader
	loader := NewFormulaLoader(nil)
	if loader == nil {
		t.Error("NewFormulaLoader returned nil")
	}

	// Test that it returns a FormulaLoader
	if _, ok := loader.(*FormulaLoader); !ok {
		t.Error("NewFormulaLoader did not return a *FormulaLoader")
	}
}

func TestFormulaLoader_LoadLlarFormula(t *testing.T) {
	// Create ixgo context
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)

	// Create loader
	loader := NewFormulaLoader(ctx)

	// Load formula_llar.gox
	testdataPath := filepath.Join("testdata", "formula", "hello_llar.gox")
	elem, err := loader.Load(testdataPath)
	if err != nil {
		t.Fatalf("Failed to load formula: %v", err)
	}

	if elem == nil {
		t.Fatal("Loaded StructElem is nil")
	}

	// Test reading fields from the loaded formula
	// Based on the formula file, we expect fields like id, fromVer, onRequire, onBuild
	t.Run("ReadFields", func(t *testing.T) {
		// Try to read the id field
		id := elem.Value("modID")

		if id != "DaveGamble/cJSON" {
			t.Fatalf("Unexpected id value: want %s got %s", "DaveGamble/cJSON", id)
			return
		}
		t.Logf("id field value: %v (type: %T)", id, id)

		// Try to read the fromVer field
		fromVer := elem.Value("modFromVer")
		if fromVer != "v1.0.0" {
			t.Fatalf("Unexpected fromVer value: want %s got %s", "v1.0.0", fromVer)
			return
		}
		t.Logf("fromVer field value: %v (type: %T)", fromVer, fromVer)
	})
}

func TestFormulaLoader_LoadCmpFormula(t *testing.T) {
	// Create ixgo context
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)

	// Create loader
	loader := NewFormulaLoader(ctx)

	// Load formula_cmp.gox
	testdataPath := filepath.Join("testdata", "cmp", "hello_cmp.gox")
	elem, err := loader.Load(testdataPath)
	if err != nil {
		t.Fatalf("Failed to load formula: %v", err)
	}

	if elem == nil {
		t.Fatal("Loaded StructElem is nil")
	}

	// Test reading fields from the loaded formula
	t.Run("ReadFields", func(t *testing.T) {
		// Try to read the compareVer field
		compareVer := elem.Value("fCompareVer").(module.VersionComparator)

		if ret := compareVer("", ""); ret != -1 {
			t.Fatalf("Unexpected compare result: want %d got %d", -1, ret)
			return
		}
		t.Logf("compareVer field value: %v (type: %T)", compareVer, compareVer)
	})
}

func TestFormulaLoader_SetValue(t *testing.T) {
	// Create ixgo context
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)

	// Create loader
	loader := NewFormulaLoader(ctx)

	// Load formula
	testdataPath := filepath.Join("testdata", "formula", "hello_llar.gox")
	elem, err := loader.Load(testdataPath)
	if err != nil {
		t.Fatalf("Failed to load formula: %v", err)
	}

	// Test setting a field value
	t.Run("SetField", func(t *testing.T) {
		// Read original value
		originalID := elem.Value("modID")
		t.Logf("Original id: %v", originalID)

		// Set a new value
		newID := "test/modified"
		elem.SetValue("modID", newID)

		// Read the modified value
		modifiedID := elem.Value("modID")
		t.Logf("Modified id: %v", modifiedID)

		// Verify the value was changed
		if modifiedID != newID {
			t.Errorf("SetValue failed: got %v, want %v", modifiedID, newID)
		}
	})
}
