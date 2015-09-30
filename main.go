package patch

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Operation is the external representation of a change to be applied
type Operation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
	From  string          `json:"from"`
}

// commands are the internal representation of an operation to be applied
// with context derived from the root object.
type command struct {
	path    []string
	pathLen int
	current interface{}
	parent  interface{}
	parents []interface{}
	key     string
	value   interface{}
}

type operator func(interface{}, *Operation, *command) (interface{}, error)

var impls = map[string]operator{
	"add":     applyAdd,
	"remove":  applyRemove,
	"replace": applyReplace,
	"move":    applyMove,
	"test":    applyTest,
	"copy":    applyCopy,
}

func Patch(o interface{}, operations []Operation) (interface{}, error) {

	o2 := deepCopy(o)

	for _, op := range operations {
		impl := impls[op.Op]
		if impl == nil {
			return nil, fmt.Errorf("%s is not valid operator", op.Op)
		}

		c, err := makeCommand(o2, &op)
		if err != nil {
			return nil, err
		}

		o2, err = impl(o2, &op, c)
		if err != nil {
			return nil, err
		}
	}

	return o2, nil
}

func makeCommand(root interface{}, op *Operation) (*command, error) {
	value, err := getOperatorValue(op)
	if err != nil {
		return nil, err
	}
	path, err := parsePath(op.Path)
	if err != nil {
		return nil, err
	}
	pathLen := len(path)
	if pathLen == 0 {
		return &command{
			path:    path,
			pathLen: pathLen,
			key:     "",
			value:   value,
			current: root,
			parent:  nil,
			parents: nil,
		}, nil
	}
	key := path[pathLen-1]

	elements, err := walkPath(root, path)
	if err != nil {
		return nil, err
	}
	return &command{
		path:    path,
		pathLen: pathLen,
		key:     key,
		value:   value,
		current: elements[pathLen],
		parent:  elements[pathLen-1],
		parents: elements[:pathLen-1],
	}, nil
}

func getOperatorValue(op *Operation) (interface{}, error) {
	if op.Value == nil {
		if op.Op == "add" || op.Op == "replace" || op.Op == "test" {
			return nil, fmt.Errorf("missing 'value' parameter")
		}
	}
	var result interface{}
	json.Unmarshal(op.Value, &result)
	return result, nil
}

func parsePath(s string) ([]string, error) {
	parts := strings.Split(s, "/")[1:]
	if len(parts) == 0 {
		return parts, nil
	}
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = strings.Replace(strings.Replace(part, "~1", "/", -1), "~0", "~", -1)
	}
	return out, nil
}

func applyAdd(root interface{}, op *Operation, c *command) (interface{}, error) {
	if len(c.path) == 0 {
		return c.value, nil
	}
	switch c.parent.(type) {
	case map[string]interface{}:
		m := c.parent.(map[string]interface{})
		m[c.key] = c.value
		return root, nil
	case []interface{}:
		s := c.parent.([]interface{})
		i, err := parseIndex(c.key, len(s), true)
		if err != nil {
			return nil, err
		}

		s = append(s, nil)
		copy(s[i+1:], s[i:])
		s[i] = c.value

		if root, err := swapParentSlice(root, s, c); err != nil {
			return nil, fmt.Errorf("Failed to swap in new slice")
		} else {
			return root, nil
		}
	}

	return nil, fmt.Errorf("Cannot set key %s in a %T", c.key, c.parent)
}

func applyRemove(root interface{}, op *Operation, c *command) (interface{}, error) {
	switch c.parent.(type) {
	case map[string]interface{}:
		m := c.parent.(map[string]interface{})
		delete(m, c.key)
		return root, nil
	case []interface{}:
		s := c.parent.([]interface{})
		i, err := strconv.Atoi(c.key)
		if err != nil || i > len(s) {
			return nil, fmt.Errorf("Invalid array index %s", c.key)
		}
		s2 := make([]interface{}, len(s)-1)
		copy(s2, s[0:i])
		copy(s2[i:], s[i+1:])

		return swapParentSlice(root, s2, c)
	}

	return nil, fmt.Errorf("Cannot remove from a %T", c.parent)
}

func applyReplace(root interface{}, op *Operation, c *command) (interface{}, error) {
	if len(c.path) == 0 {
		return c.value, nil
	}
	switch c.parent.(type) {
	case map[string]interface{}:
		m := c.parent.(map[string]interface{})
		m[c.key] = c.value
		return root, nil
	case []interface{}:
		s := c.parent.([]interface{})
		i, err := parseIndex(c.key, len(s), false)
		if err != nil {
			return nil, err
		}
		s[i] = c.value
		return root, nil
	}
	return nil, fmt.Errorf("Cannot replace %s in a %T", c.key, c.parent)
}

func applyMove(root interface{}, op *Operation, c *command) (interface{}, error) {
	if op.From == "" {
		return nil, fmt.Errorf("missing parameter 'from'")
	}
	rmOp := Operation{
		Op:   "remove",
		Path: op.From,
	}
	rmContext, err := makeCommand(root, &rmOp)
	if err != nil {
		return nil, err
	}
	root, err = applyRemove(root, &rmOp, rmContext)
	if err != nil {
		return nil, err
	}

	stringVal, err := json.Marshal(rmContext.current)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal %v to JSON (should never happen)", rmContext.current)
	}

	addOp := Operation{
		Op:    "add",
		Path:  op.Path,
		Value: json.RawMessage(stringVal),
	}
	addContext, err := makeCommand(root, &addOp)
	if err != nil {
		return nil, err
	}
	return applyAdd(root, &addOp, addContext)
}

// this is just applyMove without actually executing the move, so also way too
// slow.
func applyCopy(root interface{}, op *Operation, c *command) (interface{}, error) {
	if op.From == "" {
		return nil, fmt.Errorf("missing parameter 'from'")
	}
	rmOp := Operation{
		Op:   "remove",
		Path: op.From,
	}
	rmContext, err := makeCommand(root, &rmOp)
	if err != nil {
		return nil, err
	}

	stringVal, err := json.Marshal(rmContext.current)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal %v to JSON (should never happen)", rmContext.current)
	}

	addOp := Operation{
		Op:    "add",
		Path:  op.Path,
		Value: json.RawMessage(stringVal),
	}
	addContext, err := makeCommand(root, &addOp)
	if err != nil {
		return nil, err
	}
	return applyAdd(root, &addOp, addContext)
}

func applyTest(root interface{}, op *Operation, c *command) (interface{}, error) {
	if reflect.DeepEqual(c.current, c.value) {
		return root, nil
	}
	return nil, fmt.Errorf("%s expected to be %v, found %v", c.path, c.value, c.current)
}

func parseIndex(s string, max int, allowDash bool) (int, error) {
	if allowDash && s == "-" {
		//fmt.Printf("Parsed \"-\" array index...\n")
		return max, nil
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return -1, err
	}
	if i > max || i < 0 {
		return -1, fmt.Errorf("Array index %d out of bounds", i)
	}
	//fmt.Printf("Parsed array index %d...\n", i)
	return i, nil
}

func swapParentSlice(root interface{}, newParent []interface{}, c *command) (interface{}, error) {
	if c.pathLen > 1 {
		gp := c.parents[c.pathLen-2]
		k := c.path[c.pathLen-2]
		// fmt.Printf("Setting %k in %v to %v\n", k, gp, newParent)
		switch gp.(type) {
		case map[string]interface{}:
			m := gp.(map[string]interface{})
			m[k] = newParent
			return root, nil
		case []interface{}:
			s := gp.([]interface{})
			i, err := parseIndex(k, len(s), false)
			if err != nil {
				return nil, err
			}
			s[i] = newParent
			return root, nil
		}

		// why this should never happen:
		//  - `gp` is by definition a value that we indexed into earlier to get the
		//    descending slice element we now want to replace
		return nil, fmt.Errorf("Cannot index a %T (this should never happen)", c)
	} else if c.pathLen == 1 {
		// the slice to be replaced _is_ the root
		return newParent, nil
	}
	return nil, fmt.Errorf("zero-length path invalid")
}

func walkPath(root interface{}, path []string) ([]interface{}, error) {
	elements := make([]interface{}, len(path)+1)
	elements[0] = root
	current := root
	for i, key := range path {
		switch current.(type) {
		case map[string]interface{}:
			elements[i+1] = current.(map[string]interface{})[key]
			current = elements[i+1]
		case []interface{}:
			s := current.([]interface{})
			if j, err := parseIndex(key, len(s), true); err != nil {
				return nil, err
			} else {
				if j < len(s) {
					elements[i+1] = s[j]
				} else {
					elements[i+1] = nil
				}
				current = elements[i+1]
			}
		default:
			return nil, fmt.Errorf("Cannot index a %T", current)
		}
	}
	return elements, nil
}

/**
 * Cheapish deep-copy, this does not copy strings because strings inside an
 * interface{} are treated as immutable anyways
 */
func deepCopy(root interface{}) interface{} {
	switch root.(type) {
	case map[string]interface{}:
		m := root.(map[string]interface{})
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			out[k] = deepCopy(v)
		}
		return out
	case []interface{}:
		s := root.([]interface{})
		out := make([]interface{}, len(s))
		for k, v := range s {
			out[k] = deepCopy(v)
		}
		return out
	}
	return root
}
