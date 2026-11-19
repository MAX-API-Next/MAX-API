// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"reflect"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

type taskPrivateUnmodeled struct{ fields map[string]any }
type taskPrivateNested struct{ value any }
type taskPrivateMap map[string]any
type taskPrivateArray struct {
	original, preserved []any
	typ                 reflect.Type
}

// Anonymous projections must retain the same scalar scanner behavior as
// Task.PrivateData's explicit json column tag; unknown history is not a relation.
func (TaskPrivateData) GormDataType() string { return "json" }

// Preserve unknown history at every typed object boundary. Known fields still
// follow their existing typed marshal/omitempty behavior and validation.
func (p *TaskPrivateData) UnmarshalJSON(raw []byte) error {
	type plain TaskPrivateData
	var next plain
	if err := common.Unmarshal(raw, &next); err != nil {
		return err
	}
	value, err := decodeTaskPrivateReferenceJSON(raw)
	if err != nil {
		return err
	}
	if object, ok := value.(map[string]any); ok {
		if fields := unknownTaskPrivateFields(object, reflect.TypeOf(next)); len(fields) != 0 {
			next.unmodeled = &taskPrivateUnmodeled{fields: fields}
		}
	}
	*p = TaskPrivateData(next)
	return nil
}

func (p TaskPrivateData) MarshalJSON() ([]byte, error) {
	type plain TaskPrivateData
	raw, err := common.Marshal(plain(p))
	if err != nil || p.unmodeled == nil {
		return raw, err
	}
	var current map[string]any
	if err := common.UnmarshalUseNumber(raw, &current); err != nil {
		return nil, err
	}
	merged, err := mergeTaskPrivateFields(p.unmodeled.fields, current)
	if err != nil {
		return nil, err
	}
	return common.Marshal(merged)
}

func unknownTaskPrivateFields(object map[string]any, typ reflect.Type) map[string]any {
	unknown := make(map[string]any)
	for key, value := range canonicalTaskPrivateFields(object, typ) {
		_, field, known := taskPrivateJSONField(typ, key)
		if !known {
			unknown[key] = value
			continue
		}
		if nested := unknownTaskPrivateValue(value, field); nested != nil {
			unknown[key] = taskPrivateNested{value: nested}
		}
	}
	return unknown
}

// encoding/json uses exact names first, then Unicode SimpleFold (EqualFold),
// which intentionally differs from strings.ToLower for dotted-I and long-S.
func taskPrivateJSONField(typ reflect.Type, key string) (string, reflect.Type, bool) {
	typ = taskPrivateJSONType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return "", nil, false
	}
	var foldedName string
	var foldedType reflect.Type
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if name == key {
			return name, field.Type, true
		}
		if foldedType == nil && strings.EqualFold(name, key) {
			foldedName, foldedType = name, field.Type
		}
	}
	return foldedName, foldedType, foldedType != nil
}

func taskPrivateJSONType(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

func taskPrivateObjectFieldType(typ reflect.Type, key string) reflect.Type {
	if container := taskPrivateJSONType(typ); container != nil && container.Kind() == reflect.Map {
		return container.Elem()
	}
	_, field, _ := taskPrivateJSONField(typ, key)
	return field
}

// Canonical typed names are preferred over their historical aliases. If
// several aliases exist, use the same representative on every read.
func canonicalTaskPrivateFields(object map[string]any, typ reflect.Type) map[string]any {
	if typ = taskPrivateJSONType(typ); typ == nil || typ.Kind() != reflect.Struct {
		return object
	}
	fields := make(map[string]any, len(object))
	spelling := make(map[string]string, len(object))
	for key, value := range object {
		name := key
		if canonical, _, known := taskPrivateJSONField(typ, key); known {
			name = canonical
		}
		current, exists := spelling[name]
		if exists && key != name && (current == name || current < key) {
			continue
		}
		fields[name], spelling[name] = value, key
	}
	return fields
}

func unknownTaskPrivateValue(value any, typ reflect.Type) any {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if object, ok := value.(map[string]any); ok {
		if typ.Kind() == reflect.Struct {
			if fields := unknownTaskPrivateFields(object, typ); len(fields) != 0 {
				return fields
			}
		} else if typ.Kind() == reflect.Map {
			fields := map[string]any{}
			for key, child := range object {
				if nested := unknownTaskPrivateValue(child, typ.Elem()); nested != nil {
					fields[key] = taskPrivateNested{value: nested}
				}
			}
			if len(fields) != 0 {
				return taskPrivateMap(fields)
			}
		}
	}
	if array, ok := value.([]any); ok && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array) {
		fields, found := make([]any, len(array)), false
		for i, child := range array {
			fields[i] = unknownTaskPrivateValue(child, typ.Elem())
			found = found || fields[i] != nil
		}
		if found {
			return taskPrivateArray{original: array, preserved: fields, typ: typ}
		}
	}
	return nil
}

func mergeTaskPrivateFields(unknown, current map[string]any) (map[string]any, error) {
	merged := make(map[string]any, len(unknown)+len(current))
	for key, value := range unknown {
		merged[key] = value
	}
	for key, value := range current {
		// Typed JSON keys are canonical. Retain nested unknown fields even when
		// the stored spelling used case-insensitive historical aliases.
		for storedKey, previous := range merged {
			if !strings.EqualFold(storedKey, key) {
				continue
			}
			if nested, ok := previous.(taskPrivateNested); ok {
				var err error
				value, err = mergeTaskPrivateValue(nested.value, value)
				if err != nil {
					return nil, err
				}
			}
			delete(merged, storedKey)
		}
		merged[key] = value
	}
	for _, value := range merged {
		if _, unresolved := value.(taskPrivateNested); unresolved {
			return nil, errors.New("cannot discard unmodeled task private history from a removed typed field")
		}
	}
	return merged, nil
}

func mergeTaskPrivateValue(unknown, current any) (any, error) {
	if previous, ok := unknown.(taskPrivateMap); ok {
		if next, ok := current.(map[string]any); ok {
			merged := make(map[string]any, len(next))
			for key, value := range next {
				merged[key] = value
			}
			for key, value := range previous {
				actual, exists := next[key]
				if !exists {
					return nil, errors.New("cannot discard unmodeled task private history from a removed map entry")
				}
				nested := value.(taskPrivateNested)
				var err error
				merged[key], err = mergeTaskPrivateValue(nested.value, actual)
				if err != nil {
					return nil, err
				}
			}
			return merged, nil
		}
	}
	if previous, ok := unknown.(map[string]any); ok {
		if next, ok := current.(map[string]any); ok {
			return mergeTaskPrivateFields(previous, next)
		}
	}
	if previous, ok := unknown.(taskPrivateArray); ok {
		if next, ok := current.([]any); ok {
			before, err := previous.typed(previous.original)
			if err != nil {
				return nil, err
			}
			after, err := previous.typed(next)
			if err != nil {
				return nil, err
			}
			unchanged := reflect.DeepEqual(before, after)
			oldComponents, isComponentList := before.([]types.TaskBillingPlanComponent)
			newComponents, _ := after.([]types.TaskBillingPlanComponent)
			merged := append([]any(nil), next...)
			for i, preserved := range previous.preserved {
				if preserved == nil {
					continue
				}
				index := i
				if !unchanged {
					index = -1
					if isComponentList {
						component := oldComponents[i]
						if component.Key != "" && uniqueTaskComponentIndex(oldComponents, component.Key) == i {
							index = uniqueTaskComponentIndex(newComponents, component.Key)
							if index >= 0 && component != newComponents[index] {
								index = -1 // The same key can appear in a different plan.
							}
						}
					}
				}
				if index < 0 {
					return nil, errors.New("cannot match unmodeled task private history to the rewritten array")
				}
				merged[index], err = mergeTaskPrivateValue(preserved, merged[index])
				if err != nil {
					return nil, err
				}
			}
			return merged, nil
		}
	}
	return nil, errors.New("cannot discard unmodeled task private history from an incompatible typed value")
}

func (history taskPrivateArray) typed(entries []any) (any, error) {
	raw, err := common.Marshal(entries)
	if err != nil {
		return nil, err
	}
	value := reflect.New(history.typ)
	if err := common.Unmarshal(raw, value.Interface()); err != nil {
		return nil, err
	}
	return value.Elem().Interface(), nil
}

func uniqueTaskComponentIndex(components []types.TaskBillingPlanComponent, key string) int {
	index := -1
	for i, component := range components {
		if component.Key != key {
			continue
		}
		if index >= 0 {
			return -1
		}
		index = i
	}
	return index
}

// Decode before a map can collapse repeated members. Conflicting duplicate
// object members outside canonical fact maps are ambiguous historical data;
// equal duplicates remain readable, including case-insensitive identity aliases.
func decodeTaskPrivateReferenceJSON(raw []byte) (any, error) {
	decoder := common.NewJSONDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func([]string, int, reflect.Type) (any, error)
	read = func(path []string, depth int, typ reflect.Type) (any, error) {
		if depth > 10000 {
			return nil, errors.New("task private JSON is too deeply nested")
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return token, nil
		}
		switch delimiter {
		case '{':
			object := make(map[string]any)
			folded := make(map[string]any)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := token.(string)
				if !ok {
					return nil, errors.New("invalid task private JSON member")
				}
				childType := taskPrivateObjectFieldType(typ, key)
				value, err := read(append(path, key), depth+1, childType)
				if err != nil {
					return nil, err
				}
				canonical := key
				if !taskPrivateFactPath(path) {
					canonical = taskPrivateMemberKey(typ, key)
				}
				if previous, exists := folded[canonical]; exists && !taskPrivateJSONValueEqual(previous, value, childType) {
					return nil, errors.New("conflicting task private JSON members")
				}
				folded[canonical] = value
				object[key] = value
			}
			_, err := decoder.Token()
			return object, err
		case '[':
			array := []any{}
			var childType reflect.Type
			if container := taskPrivateJSONType(typ); container != nil && (container.Kind() == reflect.Slice || container.Kind() == reflect.Array) {
				childType = container.Elem()
			}
			for decoder.More() {
				value, err := read(path, depth+1, childType)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			_, err := decoder.Token()
			return array, err
		default:
			return nil, errors.New("invalid task private JSON delimiter")
		}
	}
	value, err := read(nil, 0, reflect.TypeOf(TaskPrivateData{}))
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after task private JSON")
	}
	return value, nil
}

func taskPrivateJSONValueEqual(a, b any, typ reflect.Type) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok || x == y {
			return ok
		}
		xDigits, xExponent := taskPrivateNumberParts(string(x))
		yDigits, yExponent := taskPrivateNumberParts(string(y))
		return xDigits == yDigits && xExponent.Cmp(yExponent) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return false
		}
		x, y = canonicalTaskPrivateFields(x, typ), canonicalTaskPrivateFields(y, typ)
		if len(x) != len(y) {
			return false
		}
		for name, value := range x {
			other, present := y[name]
			if !present || !taskPrivateJSONValueEqual(value, other, taskPrivateObjectFieldType(typ, name)) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		var entryType reflect.Type
		if container := taskPrivateJSONType(typ); container != nil && (container.Kind() == reflect.Slice || container.Kind() == reflect.Array) {
			entryType = container.Elem()
		}
		for i := range x {
			if !taskPrivateJSONValueEqual(x[i], y[i], entryType) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// Separate the significand from its base-ten exponent without materializing
// any trailing zeros, even for historical numbers with very large exponents.
func taskPrivateNumberParts(number string) (string, *big.Int) {
	exponent := new(big.Int)
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		exponent.SetString(number[i+1:], 10) // The decoder validated its grammar.
		number = number[:i]
	}
	sign := ""
	if number[0] == '-' {
		sign, number = "-", number[1:]
	}
	digits := number
	shift := 0
	if dot := strings.IndexByte(number, '.'); dot >= 0 {
		shift = dot + 1 - len(number)
		digits = number[:dot] + number[dot+1:]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0", exponent.SetInt64(0)
	}
	normalized := strings.TrimRight(digits, "0")
	shift += len(digits) - len(normalized)
	return sign + normalized, exponent.Add(exponent, big.NewInt(int64(shift)))
}

func taskPrivateFactPath(path []string) bool {
	if len(path) >= 2 && strings.EqualFold(path[0], "billing_context") && strings.EqualFold(path[1], "other_ratios") {
		return true
	}
	if len(path) < 3 || !strings.EqualFold(path[0], "billing_context") {
		return false
	}
	if strings.EqualFold(path[1], "task_billing") && (strings.EqualFold(path[2], "fields") || strings.EqualFold(path[2], "matched")) {
		return true
	}
	if strings.EqualFold(path[1], "pricing_usage_envelope") && strings.EqualFold(path[2], "facts") {
		return true
	}
	return len(path) >= 4 && strings.EqualFold(path[1], "task_billing_plan") && strings.EqualFold(path[2], "expression_plan") && strings.EqualFold(path[3], "submit_facts")
}

func taskPrivateMemberKey(typ reflect.Type, key string) string {
	if name, _, known := taskPrivateJSONField(typ, key); known {
		return name
	}
	if typ = taskPrivateJSONType(typ); typ != nil && typ.Kind() == reflect.Map {
		return key
	}
	for _, name := range []string{"plugin_key", "package_digest", "selected_plugin_key", "version", "plugin_version", "plugin_binding", "billing_context", "task_billing_plan", "expression_plan", "pricing_usage_envelope"} {
		if strings.EqualFold(name, key) {
			return name
		}
	}
	return key
}
