package bifrost

import (
	"errors"

	"github.com/bytedance/sonic"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

// errDecisionNoContent is returned when an emulating chat response carries
// neither the emit_decision tool call nor a JSON-object message body.
var errDecisionNoContent = errors.New("model returned no decision tool call or structured output")

// isUnsupportedOperation reports whether an error is a provider's
// "operation not supported" signal (set by NewUnsupportedOperationError).
func isUnsupportedOperation(err *schemas.BifrostError) bool {
	return err != nil && err.Error != nil && err.Error.Code != nil && *err.Error.Code == "unsupported_operation"
}

// decisionSystemPrompt frames the judgment task for an emulating LLM.
const decisionSystemPrompt = "You are a judgment engine. Read the given state and answer every question by " +
	"calling the provided function exactly once. For each question emit the requested value and your " +
	"confidence from 0 to 1. Base every answer only on the state; do not invent facts."

// emulateDecisionViaChat answers a decision request through a general chat model
// when the provider has no native decision support. It encodes the questions as
// a forced function tool (or native json_schema where the model advertises
// structured-output support), calls the provider's own ChatCompletion, and maps
// the tool-call arguments back to the neutral DecisionResponse shape. Used for
// both the primary path (an LLM named as the decision model) and fallbacks (an
// LLM after the native provider fails) - both flow through the same dispatch case.
func (bifrost *Bifrost) emulateDecisionViaChat(
	ctx *schemas.BifrostContext,
	provider schemas.Provider,
	key schemas.Key,
	req *schemas.BifrostDecisionRequest,
) (*schemas.BifrostDecisionResponse, *schemas.BifrostError) {
	if req == nil || len(req.Questions) == 0 {
		return nil, providerUtils.NewBifrostBadRequestError("decision request requires at least one question")
	}

	tool, err := providerUtils.BuildDecisionTool(req.Questions)
	if err != nil {
		return nil, providerUtils.NewBifrostBadRequestError(err.Error())
	}

	// State as the user message: string verbatim, structured as sorted JSON.
	stateText, marshalErr := decisionStateText(req.State)
	if marshalErr != nil {
		return nil, providerUtils.NewBifrostBadRequestError("decision state could not be serialized: " + marshalErr.Error())
	}

	sysRole := schemas.ChatMessageRoleSystem
	userRole := schemas.ChatMessageRoleUser
	sysContent := decisionSystemPrompt
	chatReq := &schemas.BifrostChatRequest{
		Provider: req.Provider,
		Model:    req.Model,
		Input: []schemas.ChatMessage{
			{Role: sysRole, Content: &schemas.ChatMessageContent{ContentStr: &sysContent}},
			{Role: userRole, Content: &schemas.ChatMessageContent{ContentStr: &stateText}},
		},
		Params: &schemas.ChatParameters{
			Tools: []schemas.ChatTool{*tool},
			ToolChoice: &schemas.ChatToolChoice{
				ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
					Type:     schemas.ChatToolChoiceTypeFunction,
					Function: &schemas.ChatToolChoiceFunction{Name: providerUtils.DecisionToolName},
				},
			},
		},
	}

	chatResp, chatErr := provider.ChatCompletion(ctx, key, chatReq)
	if chatErr != nil {
		return nil, chatErr
	}

	argsJSON, extractErr := extractDecisionToolArguments(chatResp)
	if extractErr != nil {
		return nil, providerUtils.NewBifrostOperationError(extractErr.Error(), nil)
	}

	answers, parseErr := providerUtils.ParseDecisionAnswers([]byte(argsJSON), req.Questions)
	if parseErr != nil {
		return nil, providerUtils.NewBifrostOperationError(parseErr.Error(), nil)
	}

	resp := &schemas.BifrostDecisionResponse{
		Model:   modelForDecisionResponse(chatResp, req),
		Answers: answers,
	}
	if chatResp.Usage != nil {
		resp.Usage = chatResp.Usage
	}
	// Provider/Model on ExtraFields drive downstream cost calculation.
	resp.ExtraFields.Provider = req.Provider
	resp.ExtraFields.OriginalModelRequested = req.Model
	return resp, nil
}

// decisionStateText renders the state into a chat message body.
func decisionStateText(state interface{}) (string, error) {
	if s, ok := state.(string); ok {
		return s, nil
	}
	raw, err := providerUtils.MarshalSorted(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// modelForDecisionResponse prefers the model the chat response reported (the
// resolved model after any provider-side handoff), falling back to the request.
func modelForDecisionResponse(chatResp *schemas.BifrostChatResponse, req *schemas.BifrostDecisionRequest) string {
	if chatResp != nil && chatResp.Model != "" {
		return chatResp.Model
	}
	return req.Model
}

// extractDecisionToolArguments pulls the emit_decision tool-call arguments from
// the assistant message. Falls back to plain message content for models that
// answered via native structured output instead of a tool call.
func extractDecisionToolArguments(chatResp *schemas.BifrostChatResponse) (string, error) {
	if chatResp == nil || len(chatResp.Choices) == 0 {
		return "", errDecisionNoContent
	}
	choice := chatResp.Choices[0]
	if choice.ChatNonStreamResponseChoice != nil && choice.ChatNonStreamResponseChoice.Message != nil {
		msg := choice.ChatNonStreamResponseChoice.Message
		if msg.ChatAssistantMessage != nil {
			for _, tc := range msg.ChatAssistantMessage.ToolCalls {
				if tc.Function.Name != nil && *tc.Function.Name == providerUtils.DecisionToolName {
					return tc.Function.Arguments, nil
				}
			}
			// Any single tool call is acceptable if it is the only one.
			if len(msg.ChatAssistantMessage.ToolCalls) == 1 {
				return msg.ChatAssistantMessage.ToolCalls[0].Function.Arguments, nil
			}
		}
		// Native structured output path: the JSON object arrives as content.
		if msg.Content != nil && msg.Content.ContentStr != nil && isJSONObject(*msg.Content.ContentStr) {
			return *msg.Content.ContentStr, nil
		}
	}
	return "", errDecisionNoContent
}

// isJSONObject reports whether s parses as a JSON object.
func isJSONObject(s string) bool {
	var obj map[string]interface{}
	return sonic.Unmarshal([]byte(s), &obj) == nil
}
