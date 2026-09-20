package utils

import (
	"fmt"
	"sort"

	"github.com/bytedance/sonic"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

// Decision emulation lets any tool-capable chat model answer a decision request.
// The question set is encoded as a single function tool whose flat result object
// carries one property per question; the model fills value + confidence (and a
// probability distribution for choice). ParseDecisionAnswers maps the tool-call
// arguments back to the neutral DecisionAnswer shape, validating each value
// against its question kind the way the typesafe provider validates native
// answers (core/providers/typesafe/decision.go).

// DecisionToolName is the synthetic function tool the model is forced to call.
const DecisionToolName = "emit_decision"

// numberSchema builds a JSON-schema fragment for a bounded/unbounded number.
func numberSchema(desc string, min, max *float64) map[string]interface{} {
	s := map[string]interface{}{"type": "number"}
	if desc != "" {
		s["description"] = desc
	}
	if min != nil {
		s["minimum"] = *min
	}
	if max != nil {
		s["maximum"] = *max
	}
	return s
}

// confidenceSchema is the shared 0..1 confidence field on every answer.
func confidenceSchema() map[string]interface{} {
	zero, one := 0.0, 1.0
	return numberSchema("Your confidence in this answer, from 0 to 1.", &zero, &one)
}

// instructionsText renders a question's instructions (string or structured) into
// a description string for the schema.
func instructionsText(instructions interface{}) string {
	if instructions == nil {
		return ""
	}
	if s, ok := instructions.(string); ok {
		return s
	}
	if raw, err := sonic.Marshal(instructions); err == nil {
		return string(raw)
	}
	return ""
}

// choiceOptions extracts the ordered option keys from a choice question's
// criteria (a map of option -> description). Sorted for deterministic schemas.
func choiceOptions(criteria interface{}) ([]string, map[string]string, error) {
	descs := map[string]string{}
	switch typed := criteria.(type) {
	case map[string]string:
		for k, v := range typed {
			descs[k] = v
		}
	case map[string]interface{}:
		for k, v := range typed {
			if s, ok := v.(string); ok {
				descs[k] = s
			} else {
				descs[k] = ""
			}
		}
	default:
		return nil, nil, fmt.Errorf("choice criteria must be a map of options")
	}
	opts := make([]string, 0, len(descs))
	for k := range descs {
		opts = append(opts, k)
	}
	sort.Strings(opts)
	return opts, descs, nil
}

// scoreLevels renders a score question's ordered criteria into a description.
func scoreLevels(criteria interface{}) string {
	var levels []interface{}
	switch typed := criteria.(type) {
	case []interface{}:
		levels = typed
	case []string:
		for _, l := range typed {
			levels = append(levels, l)
		}
	default:
		return ""
	}
	out := "Score levels (index -> meaning): "
	for i, l := range levels {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%d=%v", i, l)
	}
	return out
}

// scoreLegend builds the level index -> description legend from score criteria,
// so the emulated answer carries the same legend a native provider would.
func scoreLegend(criteria interface{}) map[string]string {
	var levels []interface{}
	switch typed := criteria.(type) {
	case []interface{}:
		levels = typed
	case []string:
		for _, l := range typed {
			levels = append(levels, l)
		}
	default:
		return nil
	}
	if len(levels) == 0 {
		return nil
	}
	legend := make(map[string]string, len(levels))
	for i, l := range levels {
		legend[fmt.Sprintf("%d", i)] = fmt.Sprintf("%v", l)
	}
	return legend
}

// BuildDecisionSchema builds the tool parameters: an object with one nested
// object property per question (value/choice + confidence, plus probabilities
// for choice). Property names are the question identifiers.
func BuildDecisionSchema(questions map[string]schemas.DecisionQuestion) (*schemas.ToolFunctionParameters, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("decision emulation requires at least one question")
	}
	names := make([]string, 0, len(questions))
	for name := range questions {
		names = append(names, name)
	}
	sort.Strings(names)

	props := schemas.NewOrderedMapWithCapacity(len(names))
	zero, one := 0.0, 1.0
	for _, name := range names {
		q := questions[name]
		desc := instructionsText(q.Instructions)
		var nested map[string]interface{}
		switch q.Kind {
		case schemas.DecisionKindNoul:
			nested = map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value":      numberSchema("Probability from 0 to 1. "+desc, &zero, &one),
					"confidence": confidenceSchema(),
				},
				"required":             []string{"value", "confidence"},
				"additionalProperties": false,
			}
		case schemas.DecisionKindChoice:
			opts, _, err := choiceOptions(q.Criteria)
			if err != nil {
				return nil, fmt.Errorf("question %q: %w", name, err)
			}
			nested = map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"choice":     map[string]interface{}{"type": "string", "enum": opts, "description": desc},
					"confidence": confidenceSchema(),
					"probabilities": map[string]interface{}{
						"type":                 "object",
						"description":          "Probability for each option; should sum to about 1.",
						"additionalProperties": map[string]interface{}{"type": "number"},
					},
				},
				"required":             []string{"choice", "confidence"},
				"additionalProperties": false,
			}
		case schemas.DecisionKindScore:
			nested = map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value":      numberSchema(desc+" "+scoreLevels(q.Criteria), nil, nil),
					"confidence": confidenceSchema(),
					"probabilities": map[string]interface{}{
						"type":                 "object",
						"description":          "Probability for each level index (\"0\", \"1\", ...); should sum to about 1.",
						"additionalProperties": map[string]interface{}{"type": "number"},
					},
				},
				"required":             []string{"value", "confidence"},
				"additionalProperties": false,
			}
		default:
			return nil, fmt.Errorf("question %q has unsupported kind %q", name, q.Kind)
		}
		props.Set(name, nested)
	}

	return &schemas.ToolFunctionParameters{
		Type:       "object",
		Properties: props,
		Required:   names,
	}, nil
}

// BuildDecisionTool wraps the decision schema in a forced-callable function tool.
func BuildDecisionTool(questions map[string]schemas.DecisionQuestion) (*schemas.ChatTool, error) {
	params, err := BuildDecisionSchema(questions)
	if err != nil {
		return nil, err
	}
	desc := "Emit the decision for every question. Fill each field from the given state."
	return &schemas.ChatTool{
		Type: schemas.ChatToolTypeFunction,
		Function: &schemas.ChatToolFunction{
			Name:        DecisionToolName,
			Description: &desc,
			Parameters:  params,
		},
	}, nil
}

// emulatedAnswer is the per-question object the model returns.
type emulatedAnswer struct {
	Value         interface{}        `json:"value"`
	Choice        *string            `json:"choice"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// ParseDecisionAnswers decodes the tool-call arguments and validates each answer
// against its question kind, producing the neutral DecisionAnswer map. Every
// requested question must be answered; a missing, wrong-typed, or out-of-range
// answer is an error so the request falls through to the next fallback rather
// than returning a fabricated result.
func ParseDecisionAnswers(argumentsJSON []byte, questions map[string]schemas.DecisionQuestion) (map[string]schemas.DecisionAnswer, error) {
	var raw map[string]emulatedAnswer
	if err := sonic.Unmarshal(argumentsJSON, &raw); err != nil {
		return nil, fmt.Errorf("decision tool-call arguments are not a JSON object: %w", err)
	}

	answers := make(map[string]schemas.DecisionAnswer, len(questions))
	for name, q := range questions {
		got, ok := raw[name]
		if !ok {
			return nil, fmt.Errorf("model returned no answer for question %q", name)
		}
		answer := schemas.DecisionAnswer{Kind: q.Kind, Confidence: got.Confidence}
		switch q.Kind {
		case schemas.DecisionKindNoul:
			f, ok := toFloat(got.Value)
			if !ok {
				return nil, fmt.Errorf("noul answer for %q is not a number", name)
			}
			if f < 0 || f > 1 {
				return nil, fmt.Errorf("noul answer for %q is outside [0,1]: %v", name, f)
			}
			answer.Value = f
		case schemas.DecisionKindChoice:
			if got.Choice == nil {
				return nil, fmt.Errorf("choice answer for %q carries no choice", name)
			}
			opts, _, err := choiceOptions(q.Criteria)
			if err != nil {
				return nil, fmt.Errorf("question %q: %w", name, err)
			}
			if !containsString(opts, *got.Choice) {
				return nil, fmt.Errorf("choice answer %q for %q is not an allowed option", *got.Choice, name)
			}
			answer.Value = *got.Choice
			answer.Probabilities = got.Probabilities
		case schemas.DecisionKindScore:
			f, ok := toFloat(got.Value)
			if !ok {
				return nil, fmt.Errorf("score answer for %q is not a number", name)
			}
			answer.Value = f
			answer.Legend = scoreLegend(q.Criteria)
			answer.Probabilities = got.Probabilities
		default:
			return nil, fmt.Errorf("question %q has unsupported kind %q", name, q.Kind)
		}
		answers[name] = answer
	}
	return answers, nil
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
