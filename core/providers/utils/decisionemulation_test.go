package utils

import (
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

func mixedQuestions() map[string]schemas.DecisionQuestion {
	return map[string]schemas.DecisionQuestion{
		"is_frustrated": {Kind: schemas.DecisionKindNoul, Instructions: "Is the customer frustrated?"},
		"category": {
			Kind:         schemas.DecisionKindChoice,
			Instructions: "Pick the ticket category",
			Criteria:     map[string]interface{}{"billing": "money", "bug": "defects", "other": "else"},
		},
		"urgency": {
			Kind:         schemas.DecisionKindScore,
			Instructions: "How urgent?",
			Criteria:     []interface{}{"low", "medium", "high"},
		},
	}
}

func TestBuildDecisionSchemaShape(t *testing.T) {
	params, err := BuildDecisionSchema(mixedQuestions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Marshal and re-read so we assert on the emitted JSON schema.
	raw, err := sonic.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var schema map[string]interface{}
	if err := sonic.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("top type = %v", schema["type"])
	}
	props := schema["properties"].(map[string]interface{})
	for _, name := range []string{"is_frustrated", "category", "urgency"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("missing property %q", name)
		}
	}

	noul := props["is_frustrated"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := noul["value"]; !ok {
		t.Error("noul missing value")
	}
	if _, ok := noul["confidence"]; !ok {
		t.Error("noul missing confidence")
	}

	choice := props["category"].(map[string]interface{})["properties"].(map[string]interface{})
	enum := choice["choice"].(map[string]interface{})["enum"].([]interface{})
	if len(enum) != 3 {
		t.Errorf("choice enum = %v", enum)
	}
	if _, ok := choice["probabilities"]; !ok {
		t.Error("choice missing probabilities")
	}

	score := props["urgency"].(map[string]interface{})["properties"].(map[string]interface{})
	if !strings.Contains(score["value"].(map[string]interface{})["description"].(string), "high") {
		t.Error("score value description should mention the levels")
	}
}

func TestBuildDecisionSchemaEmpty(t *testing.T) {
	if _, err := BuildDecisionSchema(map[string]schemas.DecisionQuestion{}); err == nil {
		t.Fatal("expected error for empty questions")
	}
}

func TestParseDecisionAnswersValid(t *testing.T) {
	args := `{
		"is_frustrated": {"value": 0.9, "confidence": 0.8},
		"category": {"choice": "billing", "confidence": 0.7, "probabilities": {"billing": 0.7, "bug": 0.2, "other": 0.1}},
		"urgency": {"value": 2, "confidence": 0.6}
	}`
	answers, err := ParseDecisionAnswers([]byte(args), mixedQuestions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answers["is_frustrated"].Kind != schemas.DecisionKindNoul || answers["is_frustrated"].Value.(float64) != 0.9 {
		t.Errorf("noul = %+v", answers["is_frustrated"])
	}
	if *answers["is_frustrated"].Confidence != 0.8 {
		t.Errorf("noul confidence = %v", answers["is_frustrated"].Confidence)
	}
	if answers["category"].Value.(string) != "billing" {
		t.Errorf("choice = %+v", answers["category"])
	}
	if answers["category"].Probabilities["billing"] != 0.7 {
		t.Errorf("choice probabilities = %+v", answers["category"].Probabilities)
	}
	if answers["urgency"].Value.(float64) != 2 {
		t.Errorf("score = %+v", answers["urgency"])
	}
	if answers["urgency"].Legend["2"] != "high" {
		t.Errorf("score legend = %+v", answers["urgency"].Legend)
	}
}

func TestParseDecisionAnswersRejections(t *testing.T) {
	q := mixedQuestions()
	cases := map[string]string{
		"noul out of range": `{"is_frustrated":{"value":1.4,"confidence":0.5},"category":{"choice":"billing","confidence":0.5},"urgency":{"value":1,"confidence":0.5}}`,
		"unknown choice":    `{"is_frustrated":{"value":0.5,"confidence":0.5},"category":{"choice":"nope","confidence":0.5},"urgency":{"value":1,"confidence":0.5}}`,
		"missing question":  `{"is_frustrated":{"value":0.5,"confidence":0.5},"category":{"choice":"billing","confidence":0.5}}`,
		"noul not a number": `{"is_frustrated":{"value":"high","confidence":0.5},"category":{"choice":"billing","confidence":0.5},"urgency":{"value":1,"confidence":0.5}}`,
		"malformed json":    `not json`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDecisionAnswers([]byte(args), q); err == nil {
				t.Fatalf("expected rejection for %q", name)
			}
		})
	}
}

func TestBuildDecisionToolName(t *testing.T) {
	tool, err := BuildDecisionTool(mixedQuestions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Type != schemas.ChatToolTypeFunction || tool.Function == nil || tool.Function.Name != DecisionToolName {
		t.Errorf("tool = %+v", tool)
	}
}
