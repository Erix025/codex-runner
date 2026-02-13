package miniyaml

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// This is a tiny YAML subset parser intended for simple config files.
// Supported:
// - top-level key: value (string or int)
// - top-level key: followed by list:
//     key:
//       - value
// - top-level key: followed by list of objects:
//     key:
//       - a: 1
//         b: "x"
//
// Not supported: nested maps (beyond list-of-objects), multiline scalars, anchors, etc.

type Node map[string]any

func Parse(r io.Reader) (Node, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	root := Node{}
	var currentListKey string
	var currentObj map[string]any

	for s.Scan() {
		raw := s.Text()
		line := strings.TrimRight(raw, " \t")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			currentObj = nil
			currentListKey = ""
			k, v, hasValue, err := parseKeyLine(trim)
			if err != nil {
				return nil, err
			}
			if !hasValue {
				// Start of a list.
				root[k] = []any{}
				currentListKey = k
				continue
			}
			root[k] = v
			continue
		}
		if currentListKey == "" {
			return nil, fmt.Errorf("unexpected indentation: %q", raw)
		}
		// List item?
		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			itemText := strings.TrimSpace(strings.TrimLeft(line, " "))
			itemText = strings.TrimPrefix(itemText, "- ")
			// Determine if item is scalar or inline k:v
			if strings.Contains(itemText, ":") {
				k, v, _, err := parseKeyLine(itemText)
				if err != nil {
					return nil, err
				}
				obj := map[string]any{k: v}
				currentObj = obj
				root[currentListKey] = append(root[currentListKey].([]any), obj)
			} else {
				currentObj = nil
				root[currentListKey] = append(root[currentListKey].([]any), parseScalar(itemText))
			}
			continue
		}
		// Continuation of object item.
		if currentObj == nil {
			return nil, fmt.Errorf("unexpected object field without object: %q", raw)
		}
		field := strings.TrimSpace(strings.TrimLeft(line, " "))
		k, v, hasValue, err := parseKeyLine(field)
		if err != nil {
			return nil, err
		}
		if !hasValue {
			return nil, fmt.Errorf("nested maps are not supported: %q", raw)
		}
		currentObj[k] = v
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return root, nil
}

func parseKeyLine(s string) (key string, val any, hasValue bool, err error) {
	k, rest, ok := strings.Cut(s, ":")
	if !ok {
		return "", nil, false, fmt.Errorf("expected key: value, got %q", s)
	}
	key = strings.TrimSpace(k)
	if key == "" {
		return "", nil, false, fmt.Errorf("empty key in %q", s)
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return key, nil, false, nil
	}
	return key, parseScalar(rest), true, nil
}

func parseScalar(s string) any {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return s
}
