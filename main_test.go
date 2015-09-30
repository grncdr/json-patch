package patch

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"
)

type Spec struct {
	Comment  string
	Doc      interface{}
	Patch    []Operation
	Expected interface{}
	Error    string
	Disabled bool
}

func doSpec(name string, t *testing.T, specs []Spec) {
	fmt.Printf("# %s\n", name)
	for i, spec := range specs {
		if spec.Disabled {
			continue
		}
		if spec.Doc == nil {
			spec.Doc = make(map[string]interface{})
		}
		result, err := Patch(spec.Doc, spec.Patch)
		if err == nil && spec.Error != "" {
			t.Errorf("not ok %d [%s] - expected error %s", i, spec.Comment, spec.Error)
		} else if err != nil && spec.Error == "" {
			t.Errorf("not ok %d [%s] - unexpected error %v", i, spec.Comment, err)
		} else if err != nil {
			// expected error, TODO: test error messages
		} else if spec.Expected != nil && !reflect.DeepEqual(result, spec.Expected) {
			t.Errorf("not ok %d [%s] - expected %v to equal %v", i, spec.Comment, result, spec.Expected)
		} else {
			fmt.Printf("ok %d [%s]\n", i, spec.Comment)
		}
	}
}

func doSpecFile(t *testing.T, filename string) {
	var bytes []byte
	var err error
	if bytes, err = ioutil.ReadFile(filename); err != nil {
		t.Error(err)
		return
	}
	specs := make([]Spec, 1)
	if err = json.Unmarshal(bytes, &specs); err != nil {
		t.Error(err)
		return
	}
	doSpec(filename, t, specs)
}

func TestAdd(t *testing.T) {
	doSpec("Add tests", t, []Spec{
		Spec{
			Comment: "add test 1",
			Patch: []Operation{
				Operation{Op: "add", Path: "/hello", Value: json.RawMessage(`"world"`)},
			},
			Expected: map[string]interface{}{"hello": "world"},
		},
		Spec{
			Comment: "add test 2",
			Patch: []Operation{
				Operation{Op: "add", Path: "/nested", Value: json.RawMessage(`{}`)},
				Operation{Op: "add", Path: "/nested/number", Value: json.RawMessage(`12`)},
				Operation{Op: "add", Path: "/nested/string", Value: json.RawMessage(`"yeah"`)},
			},
			Expected: map[string]interface{}{
				"nested": map[string]interface{}{
					"number": float64(12),
					"string": "yeah",
				},
			},
		},
	})
}

func TestRemove(t *testing.T) {
	doSpec("Remove tests", t, []Spec{
		Spec{
			Comment: "Remove test 1",
			Patch: []Operation{
				Operation{Op: "add", Path: "/hello", Value: json.RawMessage(`"world"`)},
				Operation{Op: "remove", Path: "/hello"},
			},
			Expected: map[string]interface{}{},
		},
		Spec{
			Comment: "Remove test 2",
			Patch: []Operation{
				Operation{Op: "add", Path: "/nested", Value: json.RawMessage(`{}`)},
				Operation{Op: "add", Path: "/nested/number", Value: json.RawMessage("12")},
				Operation{Op: "add", Path: "/nested/string", Value: json.RawMessage(`"yeah"`)},
				Operation{Op: "remove", Path: "/nested/number"},
			},
			Expected: map[string]interface{}{
				"nested": map[string]interface{}{"string": "yeah"},
			},
		},
	})
}

func TestBasicSpec(t *testing.T) {
	doSpecFile(t, "testdata/spec_tests.json")
}

func TestEvenMore(t *testing.T) {
	doSpecFile(t, "testdata/tests.json")
}
