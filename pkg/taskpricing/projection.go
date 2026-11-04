// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

const MaxProjectionResponseBytes = 1 << 20

// FactProjection is trusted exact-package host policy. Extras may be omitted
// only with the reviewed scalar value; declared facts can never be discarded.
// Only absence permits selection of a secondary raw path, never zero or null.
type FactProjection struct {
	SubmitExtras  types.TaskPricingFacts `json:"submit_extras,omitempty"`
	FinalExtras   types.TaskPricingFacts `json:"final_extras,omitempty"`
	ActualPaths   map[string][]string    `json:"actual_paths,omitempty"`
	StatusPath    string                 `json:"status_path,omitempty"`
	SuccessStates []string               `json:"success_states,omitempty"`
}

type ProjectedFacts struct {
	Facts       types.TaskPricingFacts
	DroppedKeys []string
}

var projectionPath = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)
var decimalFact = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func normalizeProjection(c types.TaskFactContract, p FactProjection) (FactProjection, error) {
	out := FactProjection{StatusPath: p.StatusPath, SuccessStates: slices.Clone(p.SuccessStates)}
	copyExtras := func(input types.TaskPricingFacts) (types.TaskPricingFacts, error) {
		if len(input) == 0 {
			return nil, nil
		}
		copy := types.TaskPricingFacts{}
		for key, value := range input {
			if !projectionPath.MatchString(key) || strings.Contains(key, ".") {
				return nil, fmt.Errorf("invalid legacy fact name")
			}
			if _, declared := c.UsageSchema[key]; declared {
				return nil, fmt.Errorf("declared fact cannot be dropped")
			}
			switch v := value.(type) {
			case bool:
				copy[key] = v
			case string:
				copy[key] = v
			default:
				n, ok := number(value)
				if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1<<53-1 {
					return nil, fmt.Errorf("invalid reviewed extra fact")
				}
				copy[key] = n
			}
		}
		return copy, nil
	}
	var err error
	if out.SubmitExtras, err = copyExtras(p.SubmitExtras); err != nil {
		return out, err
	}
	if out.FinalExtras, err = copyExtras(p.FinalExtras); err != nil {
		return out, err
	}
	if p.StatusPath != "" && (!projectionPath.MatchString(p.StatusPath) || len(p.SuccessStates) == 0) {
		return out, fmt.Errorf("invalid completion state policy")
	}
	if len(p.SuccessStates) > 0 && p.StatusPath == "" {
		return out, fmt.Errorf("completion status path required")
	}
	seenStates := map[string]bool{}
	for _, value := range p.SuccessStates {
		if value == "" || strings.TrimSpace(value) != value || seenStates[value] {
			return out, fmt.Errorf("invalid completion success states")
		}
		seenStates[value] = true
	}
	if len(p.ActualPaths) > 0 {
		out.ActualPaths = map[string][]string{}
	}
	for key, paths := range p.ActualPaths {
		if c.UsageSchema[key].Type != "number" || c.FieldPolicy[key].Phase != "actual" || len(paths) == 0 || p.StatusPath == "" {
			return out, fmt.Errorf("raw paths require actual numeric facts and completion policy")
		}
		seen := map[string]bool{}
		for _, path := range paths {
			if len(path) > 256 || !projectionPath.MatchString(path) || seen[path] {
				return out, fmt.Errorf("invalid raw usage path")
			}
			seen[path] = true
		}
		out.ActualPaths[key] = slices.Clone(paths)
	}
	return out, nil
}

func projectFacts(c types.TaskFactContract, extras, facts types.TaskPricingFacts) (ProjectedFacts, error) {
	out := ProjectedFacts{Facts: types.TaskPricingFacts{}}
	for key, value := range facts {
		if _, declared := c.UsageSchema[key]; declared {
			out.Facts[key] = value
			continue
		}
		expected, allowed := extras[key]
		if !allowed {
			return ProjectedFacts{}, fmt.Errorf("undeclared plugin fact %q", key)
		}
		matches := false
		switch wanted := expected.(type) {
		case string:
			actual, ok := value.(string)
			matches = ok && actual == wanted
		case bool:
			actual, ok := value.(bool)
			matches = ok && actual == wanted
		case float64:
			actual, ok := number(value)
			matches = ok && actual == wanted
		}
		if !matches {
			return ProjectedFacts{}, fmt.Errorf("legacy plugin fact %q differs from reviewed projection", key)
		}
		out.DroppedKeys = append(out.DroppedKeys, key)
	}
	slices.Sort(out.DroppedKeys)
	return out, nil
}

func (a ApprovedPhaseContract) ProjectSubmit(facts types.TaskPricingFacts) (ProjectedFacts, error) {
	c, err := NormalizeContract(a.Contract)
	if err != nil {
		return ProjectedFacts{}, err
	}
	p, err := normalizeProjection(c, a.Projection)
	if err != nil {
		return ProjectedFacts{}, err
	}
	out, err := projectFacts(c, p.SubmitExtras, facts)
	if err != nil {
		return ProjectedFacts{}, err
	}
	out.Facts, err = SubmitFacts(c, out.Facts)
	if err != nil {
		return ProjectedFacts{}, err
	}
	return out, nil
}

// ProjectFinal verifies hook facts against the original provider JSON. It never
// extracts a replacement charge, inherits actual estimates or settles funds.
func (a ApprovedPhaseContract) ProjectFinal(frozen, facts types.TaskPricingFacts, raw []byte) (ProjectedFacts, error) {
	c, err := NormalizeContract(a.Contract)
	if err != nil {
		return ProjectedFacts{}, err
	}
	p, err := normalizeProjection(c, a.Projection)
	if err != nil {
		return ProjectedFacts{}, err
	}
	if p.StatusPath == "" || len(raw) == 0 || len(raw) > MaxProjectionResponseBytes {
		return ProjectedFacts{}, fmt.Errorf("reviewed completion policy and bounded raw evidence required")
	}
	var root any
	if err := common.Unmarshal(raw, &root); err != nil {
		return ProjectedFacts{}, fmt.Errorf("invalid completion JSON")
	}
	status, present, err := projectionLookup(root, p.StatusPath)
	state, isString := status.(string)
	if err != nil || !present || !isString || !slices.Contains(p.SuccessStates, state) {
		return ProjectedFacts{}, fmt.Errorf("raw response is not an approved successful completion")
	}
	out, err := projectFacts(c, p.FinalExtras, facts)
	if err != nil {
		return ProjectedFacts{}, err
	}
	out.Facts, err = CompleteFacts(c, frozen, out.Facts)
	if err != nil {
		return ProjectedFacts{}, err
	}
	for key, value := range out.Facts {
		if c.FieldPolicy[key].Phase != "actual" || c.UsageSchema[key].Type != "number" {
			continue
		}
		matched := false
		for _, path := range p.ActualPaths[key] {
			rawValue, exists, err := projectionLookup(root, path)
			if err != nil {
				return ProjectedFacts{}, err
			}
			if !exists {
				continue
			}
			n, err := rawProjectionNumber(rawValue)
			if err != nil || n != value.(float64) {
				return ProjectedFacts{}, fmt.Errorf("raw completion disagrees with plugin fact %q", key)
			}
			matched = true
			break
		}
		if !matched {
			return ProjectedFacts{}, fmt.Errorf("raw evidence missing for actual fact %q", key)
		}
	}
	return out, nil
}

func projectionLookup(root any, path string) (any, bool, error) {
	value := root
	for _, name := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("invalid completion JSON object on reviewed path")
		}
		value, ok = object[name]
		if !ok {
			return nil, false, nil
		}
	}
	return value, true, nil
}

func rawProjectionNumber(value any) (float64, error) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case string:
		text := strings.TrimSpace(v)
		if !decimalFact.MatchString(text) {
			return 0, fmt.Errorf("invalid raw numeric fact")
		}
		var err error
		n, err = strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid raw numeric fact")
		}
	default:
		return 0, fmt.Errorf("raw numeric fact has wrong type")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1<<53-1 {
		return 0, fmt.Errorf("raw numeric fact out of range")
	}
	return n, nil
}
