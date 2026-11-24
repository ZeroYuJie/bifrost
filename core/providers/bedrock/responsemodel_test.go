package bedrock

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/valyala/fasthttp"
)

// TestResponseModel verifies that when the request model was resolved through
// a key alias, responses echo the caller-facing alias key instead of the wire
// identifier (an opaque resource id for application inference profiles).
func TestResponseModel(t *testing.T) {
	wireModel := "4gnd8iceukh0"

	// Alias resolved — caller-facing name wins.
	ctx := schemas.NewBifrostContext(nil, schemas.NoDeadline)
	ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{
		Key: "claude-sonnet-5-660169747010",
		Config: &schemas.AliasConfig{
			ModelID: wireModel,
		},
	})
	if got := responseModel(ctx, wireModel); got != "claude-sonnet-5-660169747010" {
		t.Errorf("alias resolved: got %q, want alias key %q", got, "claude-sonnet-5-660169747010")
	}

	// No alias — wire model passes through.
	emptyCtx := schemas.NewBifrostContext(nil, schemas.NoDeadline)
	if got := responseModel(emptyCtx, wireModel); got != wireModel {
		t.Errorf("no alias: got %q, want wire model %q", got, wireModel)
	}

	// Nil ctx — wire model passes through.
	if got := responseModel(nil, wireModel); got != wireModel {
		t.Errorf("nil ctx: got %q, want wire model %q", got, wireModel)
	}
}

// Keep the caller model on Converse events as well as the final usage chunk.
// The v2.6.0 sendEvent refactor moved ordinary chunks away from the old alias
// assignment. All traffic here is redirected to a loopback mock with a fake key.
func TestConverseResponseModel(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				name := "chat"
				if responses {
					name = "responses"
				}
				if streaming {
					name += "/stream"
				}
				if alias {
					name += "/alias"
				}
				t.Run(name, func(t *testing.T) {
					wireModel, wantModel := "anthropic.claude-sonnet-5-5", "anthropic.claude-sonnet-5-5"
					ctx := testBedrockCtx()
					if alias {
						wireModel, wantModel = "opaque-profile-id", "claude-sonnet-profile"
						ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{
							Key: wantModel, Config: &schemas.AliasConfig{
								ModelID: wireModel, ModelName: schemas.Ptr("claude-sonnet-5-5"),
							},
						})
					}
					ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						wantPath := "/model/" + wireModel + "/converse"
						if streaming {
							wantPath += "-stream"
						}
						if r.URL.Path != wantPath {
							t.Errorf("wire path = %q, want %q", r.URL.Path, wantPath)
						}
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Errorf("read mock request: %v", err)
						}
						if got := gjson.GetBytes(body, "additionalModelRequestFields.thinking").Raw; got != `{"type":"between_tools"}` {
							t.Errorf("thinking = %s, want only between_tools", got)
						}
						if gjson.GetBytes(body, "additionalModelRequestFields.output_config.effort").String() != "low" || gjson.GetBytes(body, "inferenceConfig.maxTokens").Int() != 128 {
							t.Errorf("effort or token limit changed: %s", body)
						}
						if !streaming {
							w.Header().Set("Content-Type", "application/json")
							_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":3,"outputTokens":1,"totalTokens":4}}`)
							return
						}
						w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
						writeEventStreamEvent(t, w, "messageStart", []byte(`{"role":"assistant"}`))
						writeEventStreamEvent(t, w, "contentBlockDelta", []byte(`{"delta":{"text":"hello"},"contentBlockIndex":0}`))
						writeEventStreamEvent(t, w, "contentBlockStop", []byte(`{"contentBlockIndex":0}`))
						writeEventStreamEvent(t, w, "messageStop", []byte(`{"stopReason":"end_turn"}`))
						writeEventStreamEvent(t, w, "metadata", []byte(`{"usage":{"inputTokens":3,"outputTokens":1,"totalTokens":4}}`))
					}))
					defer ts.Close()
					provider := newTestProviderWithServer(t, ts)
					chatReq := testChatRequest()
					chatReq.Model = wireModel
					chatReq.Params = &schemas.ChatParameters{MaxCompletionTokens: schemas.Ptr(128), Reasoning: &schemas.ChatReasoning{Type: schemas.Ptr("between_tools"), Effort: schemas.Ptr("low")}}
					respReq := testResponsesRequest()
					respReq.Model = wireModel
					respReq.Params = &schemas.ResponsesParameters{MaxOutputTokens: schemas.Ptr(128), Reasoning: &schemas.ResponsesParametersReasoning{Type: schemas.Ptr("between_tools"), Effort: schemas.Ptr("low")}}
					checkModel := func(model string) {
						if model != wantModel {
							t.Errorf("response model = %q, want %q", model, wantModel)
						}
					}
					if streaming {
						postHook := func(_ *schemas.BifrostContext, result *schemas.BifrostResponse, err *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError) {
							if result != nil && result.ChatResponse != nil {
								checkModel(result.ChatResponse.Model)
							}
							if result != nil && result.ResponsesStreamResponse != nil && result.ResponsesStreamResponse.Response != nil {
								checkModel(result.ResponsesStreamResponse.Response.Model)
							}
							return result, err
						}
						var stream chan *schemas.BifrostStreamChunk
						var bifrostErr *schemas.BifrostError
						if responses {
							stream, bifrostErr = provider.ResponsesStream(ctx, postHook, nil, testBedrockKey(), respReq)
						} else {
							stream, bifrostErr = provider.ChatCompletionStream(ctx, postHook, nil, testBedrockKey(), chatReq)
						}
						require.Nil(t, bifrostErr)
						models := 0
						for chunk := range stream {
							require.Nil(t, chunk.BifrostError)
							if chunk.BifrostChatResponse != nil {
								checkModel(chunk.BifrostChatResponse.Model)
								models++
							}
							if chunk.BifrostResponsesStreamResponse != nil && chunk.BifrostResponsesStreamResponse.Response != nil {
								checkModel(chunk.BifrostResponsesStreamResponse.Response.Model)
								models++
							}
						}
						require.Greater(t, models, 1, "must check lifecycle chunks as well as the final response")
					} else if responses {
						result, bifrostErr := provider.Responses(ctx, testBedrockKey(), respReq)
						require.Nil(t, bifrostErr)
						checkModel(result.Model)
					} else {
						result, bifrostErr := provider.ChatCompletion(ctx, testBedrockKey(), chatReq)
						require.Nil(t, bifrostErr)
						checkModel(result.Model)
					}
					require.Equal(t, wireModel, chatReq.Model)
					require.Equal(t, wireModel, respReq.Model)
				})
			}
		}
	}
}

// InvokeModel bypasses the Converse response converters. Keep the alias on
// unary responses and every streaming lifecycle event without changing the
// model used in the AWS URL or the request passed to the provider.
func TestInvokeResponseModel(t *testing.T) {
	const wireModel = "anthropic.claude-sonnet-4-6"
	const upstreamModel = "claude-sonnet-4-6"
	const aliasModel = "claude-sonnet-profile"
	for _, responses := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				name := "chat"
				if responses {
					name = "responses"
				}
				if streaming {
					name += "/stream"
				}
				if alias {
					name += "/alias"
				}
				t.Run(name, func(t *testing.T) {
					ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if !strings.HasPrefix(r.URL.Path, "/model/"+wireModel+"/invoke") {
							t.Errorf("wire model changed: %s", r.URL.Path)
						}
						body, err := io.ReadAll(r.Body)
						if err != nil || !gjson.GetBytes(body, "context_management.edits.0.type").Exists() {
							t.Errorf("InvokeModel compaction request missing: %s, %v", body, err)
						}
						if !streaming {
							w.Header().Set("Content-Type", "application/json")
							_, _ = io.WriteString(w, `{"id":"msg_alias","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
							return
						}
						w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
						for _, event := range []string{
							`{"type":"message_start","message":{"id":"msg_alias","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`,
							`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
							`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
							`{"type":"content_block_stop","index":0}`,
							`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
							`{"type":"message_stop"}`,
						} {
							writeInvokeChunk(t, w, event)
						}
					}))
					defer ts.Close()
					provider := newTestProviderWithServer(t, ts)
					addr := strings.TrimPrefix(ts.URL, "https://")
					// Both clients can only dial this loopback mock; no AWS
					// endpoint or local credential chain is used by these tests.
					client := &fasthttp.Client{
						Dial:                   func(string) (net.Conn, error) { return net.Dial("tcp", addr) },
						TLSConfig:              &tls.Config{InsecureSkipVerify: true},
						DisablePathNormalizing: true,
						ReadTimeout:            5 * time.Second,
						WriteTimeout:           5 * time.Second,
					}
					provider.mantleClient, provider.mantleStreamingClient = client, client
					ctx := testBedrockCtx()
					want := upstreamModel
					if alias {
						ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{
							Key: aliasModel, Config: &schemas.AliasConfig{ModelID: wireModel},
						})
						want = aliasModel
					}
					checkModel := func(model string) {
						// The upstream converter omits model on some deltas.
						if !alias && streaming && model == "" {
							return
						}
						if model != want {
							t.Errorf("response model = %q, want %q", model, want)
						}
					}
					cm := json.RawMessage(`{"edits":[{"type":"compact_20260112"}]}`)
					chatReq := testChatRequest()
					chatReq.Model = wireModel
					chatReq.Params = &schemas.ChatParameters{ContextManagement: cm, MaxCompletionTokens: schemas.Ptr(1024)}
					respReq := &schemas.BifrostResponsesRequest{
						Model:  wireModel,
						Input:  []schemas.ResponsesMessage{{Role: schemas.Ptr(schemas.ResponsesInputMessageRoleUser), Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("hello")}}},
						Params: &schemas.ResponsesParameters{ContextManagement: cm, MaxOutputTokens: schemas.Ptr(1024)},
					}
					if streaming {
						postHook := func(_ *schemas.BifrostContext, result *schemas.BifrostResponse, err *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError) {
							if result != nil && result.ChatResponse != nil {
								checkModel(result.ChatResponse.Model)
							}
							if result != nil && result.ResponsesStreamResponse != nil && result.ResponsesStreamResponse.Response != nil {
								checkModel(result.ResponsesStreamResponse.Response.Model)
							}
							return result, err
						}
						var stream chan *schemas.BifrostStreamChunk
						var bifrostErr *schemas.BifrostError
						if responses {
							stream, bifrostErr = provider.ResponsesStream(ctx, postHook, nil, testBedrockKey(), respReq)
						} else {
							stream, bifrostErr = provider.ChatCompletionStream(ctx, postHook, nil, testBedrockKey(), chatReq)
						}
						require.Nil(t, bifrostErr)
						models := 0
						for chunk := range stream {
							require.Nil(t, chunk.BifrostError)
							if chunk.BifrostChatResponse != nil {
								checkModel(chunk.BifrostChatResponse.Model)
								models++
							}
							if chunk.BifrostResponsesStreamResponse != nil && chunk.BifrostResponsesStreamResponse.Response != nil {
								checkModel(chunk.BifrostResponsesStreamResponse.Response.Model)
								models++
							}
						}
						require.Positive(t, models)
					} else if responses {
						result, bifrostErr := provider.Responses(ctx, testBedrockKey(), respReq)
						require.Nil(t, bifrostErr)
						checkModel(result.Model)
					} else {
						result, bifrostErr := provider.ChatCompletion(ctx, testBedrockKey(), chatReq)
						require.Nil(t, bifrostErr)
						checkModel(result.Model)
					}
					require.Equal(t, wireModel, chatReq.Model)
					require.Equal(t, wireModel, respReq.Model)
				})
			}
		}
	}
}

func TestResponseModelPostHookRunnerNilSafety(t *testing.T) {
	ctx := testBedrockCtx()
	ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{Key: "alias"})
	runner := responseModelPostHookRunner(ctx, nil)
	bifrostErr := &schemas.BifrostError{ExtraFields: schemas.BifrostErrorExtraFields{RoutingInfo: schemas.RoutingInfo{Model: "wire"}}}
	result, gotErr := runner(ctx, nil, bifrostErr)
	require.Nil(t, result)
	require.Same(t, bifrostErr, gotErr)
	require.Equal(t, "wire", gotErr.ExtraFields.RoutingInfo.Model)
	response := &schemas.BifrostResponse{ChatResponse: &schemas.BifrostChatResponse{Model: "wire"}}
	result, gotErr = runner(ctx, response, nil)
	require.Same(t, response, result)
	require.Nil(t, gotErr)
	require.Equal(t, "alias", result.ChatResponse.Model)
}

func BenchmarkResponseModel(b *testing.B) {
	for _, name := range []string{"baseline", "no-alias", "alias"} {
		b.Run(name, func(b *testing.B) {
			ctx := testBedrockCtx()
			if name == "alias" {
				ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{Key: "alias"})
			}
			resolve := func(_ *schemas.BifrostContext, model string) string { return model }
			if name != "baseline" {
				resolve = responseModel
			}
			response := &schemas.BifrostChatResponse{}
			b.ReportAllocs()
			for b.Loop() {
				response.Model = resolve(ctx, "wire")
			}
		})
	}
}

func BenchmarkResponseModelPostHookRunner(b *testing.B) {
	for _, name := range []string{"baseline", "no-alias", "alias"} {
		b.Run(name, func(b *testing.B) {
			ctx := testBedrockCtx()
			if name == "alias" {
				ctx.SetValue(schemas.BifrostContextKeyResolvedAlias, &schemas.ResolvedAlias{Key: "alias"})
			}
			runner := schemas.PostHookRunner(noopPostHookRunner)
			if name != "baseline" {
				runner = responseModelPostHookRunner(ctx, runner)
			}
			response := &schemas.BifrostResponse{ChatResponse: &schemas.BifrostChatResponse{Model: "wire"}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = runner(ctx, response, nil)
			}
		})
	}
}
