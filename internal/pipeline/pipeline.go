package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"ai-api-stronger/internal/config"
	"ai-api-stronger/internal/planner"
	"ai-api-stronger/internal/privacy"
	"ai-api-stronger/internal/response"
	"ai-api-stronger/internal/streaming"
	"ai-api-stronger/internal/upstream"
)

// Pipeline executes a compiled ExecutionPlan. It deliberately receives all
// routing and policy decisions from planner so request execution never reads
// YAML or re-runs model-rule selection.
type Pipeline struct {
	Clients       *upstream.Manager
	Privacy       privacy.Processor
	FakeStreaming streaming.Boundary
}

type streamingDecision struct {
	ClientWantsStream        bool
	NonstreamToStreamEnabled bool
	UseFakeStreamResponse    bool
}

func (p Pipeline) Execute(w http.ResponseWriter, r *http.Request, plan *planner.ExecutionPlan, originalBody []byte) {
	transformedRequest, err := TransformRequestBody(plan.UpstreamFormat, originalBody, plan.ModelName, plan.RequestBodyPlan)
	if err != nil {
		response.WriteError(w, response.CodeBodyProcessingFailed, err.Error())
		return
	}

	requestContext, cancel := context.WithTimeout(r.Context(), plan.Timeout)
	defer cancel()

	decision := decideStreaming(plan, r, transformedRequest.Body)
	upstreamBody, err := prepareUpstreamBody(plan, decision, transformedRequest.Body)
	if err != nil {
		writeStreamingPreparationError(w, err)
		return
	}
	if !validatePrivacyStreamingCompatibility(w, plan, decision) {
		return
	}

	upstreamHeaders := ApplyRequestHeaders(r.Header, plan.RequestHeaderPlan, plan.RequestID)
	if plan.LLMPrivateProtect {
		p.executePrivacyProtectedRequest(w, requestContext, r, plan, upstreamHeaders, upstreamBody, decision.UseFakeStreamResponse)
		return
	}

	p.executeDirectUpstreamRequest(w, requestContext, r.Method, plan, upstreamHeaders, upstreamBody, decision.UseFakeStreamResponse)
}

func decideStreaming(plan *planner.ExecutionPlan, r *http.Request, transformedBody []byte) streamingDecision {
	clientWantsStream := streaming.ClientWantsStream(plan.UpstreamFormat, upstreamTailPath(r), transformedBody)
	nonstreamToStreamEnabled := plan.Streaming != nil && plan.Streaming.Mode == config.StreamingModeNonstreamToStream
	return streamingDecision{
		ClientWantsStream:        clientWantsStream,
		NonstreamToStreamEnabled: nonstreamToStreamEnabled,
		UseFakeStreamResponse:    clientWantsStream && nonstreamToStreamEnabled,
	}
}

func prepareUpstreamBody(plan *planner.ExecutionPlan, decision streamingDecision, transformedBody []byte) ([]byte, error) {
	if !decision.NonstreamToStreamEnabled {
		return transformedBody, nil
	}
	// Effective nonstream_to_stream always sends a non-stream upstream request,
	// even when the client did not ask for SSE. The client intent only controls
	// whether the final response is fake-streamed.
	if plan.UpstreamFormat == config.FormatGemini {
		return nil, streaming.ErrUnsupportedFakeStream
	}
	return streaming.ShapeNonstreamRequest(plan.UpstreamFormat, transformedBody)
}

func validatePrivacyStreamingCompatibility(w http.ResponseWriter, plan *planner.ExecutionPlan, decision streamingDecision) bool {
	if plan.LLMPrivateProtect && decision.ClientWantsStream && !decision.UseFakeStreamResponse {
		response.WriteError(w, response.CodeStreamingConflict, "streaming request requires nonstream_to_stream when privacy is enabled")
		return false
	}
	return true
}

func writeStreamingPreparationError(w http.ResponseWriter, err error) {
	if errors.Is(err, streaming.ErrUnsupportedFakeStream) {
		response.WriteError(w, response.CodeStreamingConflict, "unsupported nonstream_to_stream format")
		return
	}
	response.WriteError(w, response.CodeBodyProcessingFailed, "failed to shape non-stream upstream request")
}

func (p Pipeline) executePrivacyProtectedRequest(w http.ResponseWriter, ctx context.Context, r *http.Request, plan *planner.ExecutionPlan, headers http.Header, body []byte, useFakeStream bool) {
	privacyProcessor := p.Privacy
	if privacyProcessor == nil {
		response.WriteError(w, response.CodeInternalError, "privacy processor is not configured")
		return
	}

	protectedResponse, err := privacyProcessor.Do(ctx, privacy.Request{
		Method:      r.Method,
		UpstreamURL: plan.UpstreamURL,
		Headers:     headers,
		Body:        body,
		SessionID:   privacy.SessionID(r.Header),
	})
	if err != nil || protectedResponse == nil {
		response.WriteError(w, response.CodeInternalError, "privacy protection failed")
		return
	}
	defer protectedResponse.Body.Close()

	p.writeBufferedResponse(w, protectedResponse, plan, useFakeStream)
}

func (p Pipeline) executeDirectUpstreamRequest(w http.ResponseWriter, ctx context.Context, method string, plan *planner.ExecutionPlan, headers http.Header, body []byte, useFakeStream bool) {
	clientManager := p.Clients
	if clientManager == nil {
		clientManager = upstream.NewManager()
	}
	managedClient, releaseClient := clientManager.Acquire(upstream.KeyFromPlan(plan))
	defer releaseClient()

	upstreamRequest, err := upstream.NewRequest(ctx, method, plan.UpstreamURL, headers, body)
	if err != nil {
		response.WriteError(w, response.CodeInternalError, "failed to build upstream request")
		return
	}
	upstreamResponse, err := managedClient.Client().Do(upstreamRequest)
	if err != nil {
		writeUpstreamRequestError(w, err)
		return
	}
	defer upstreamResponse.Body.Close()

	if useFakeStream {
		p.writeBufferedResponse(w, upstreamResponse, plan, true)
		return
	}
	_ = writeTransparentUpstreamResponse(w, upstreamResponse, plan)
}

func writeUpstreamRequestError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		response.WriteError(w, response.CodeUpstreamTimeout, "upstream timeout")
		return
	}
	response.WriteError(w, response.CodeUpstreamFailure, "upstream connection failure")
}

func (p Pipeline) writeBufferedResponse(w http.ResponseWriter, upstreamResponse *http.Response, plan *planner.ExecutionPlan, useFakeStream bool) {
	bufferLimit := planStreamBufferLimit(plan)
	bufferedBody, err := readBounded(upstreamResponse.Body, bufferLimit)
	if err != nil {
		response.WriteError(w, response.CodeUpstreamResponseTooLarge, "upstream response exceeds allowed buffer size")
		return
	}
	clonedResponse := *upstreamResponse
	clonedResponse.Body = io.NopCloser(bytes.NewReader(bufferedBody))

	// Upstream errors are still ordinary upstream responses; do not present them
	// as successful SSE streams or wrap their bodies in unified JSON.
	if !useFakeStream || upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		_ = writeTransparentUpstreamResponse(w, &clonedResponse, plan)
		return
	}

	fakeStreamWriter := newFakeStreamResponseWriter(w, clonedResponse.Header, plan.ResponseHeaderPlan, plan.ResponseStatusMap, plan.RequestID)
	if err := p.FakeStreaming.Convert(fakeStreamWriter, plan.UpstreamFormat, &clonedResponse); err != nil {
		writeFakeStreamError(w, err)
		return
	}
}

func writeTransparentUpstreamResponse(w http.ResponseWriter, upstreamResponse *http.Response, plan *planner.ExecutionPlan) error {
	err := WriteUpstreamResponse(w, upstreamResponse, plan.ResponseHeaderPlan, plan.ResponseStatusMap)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return nil
}

func writeFakeStreamError(w http.ResponseWriter, err error) {
	if errors.Is(err, streaming.ErrResponseTooLarge) {
		response.WriteError(w, response.CodeUpstreamResponseTooLarge, "upstream response exceeds allowed buffer size")
		return
	}
	response.WriteError(w, response.CodeFakeStreamFailed, "nonstream-to-stream processing failed")
}

func planStreamBufferLimit(plan *planner.ExecutionPlan) int64 {
	if plan.Streaming != nil && plan.Streaming.MaxBufferBytes > 0 {
		return plan.Streaming.MaxBufferBytes
	}
	return 0
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	limitedReader := &io.LimitedReader{R: reader, N: limit + 1}
	bufferedBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(bufferedBody)) > limit {
		return nil, errors.New("response too large")
	}
	return bufferedBody, nil
}

func upstreamTailPath(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		return r.URL.Path
	}
	return strings.Join(parts[3:], "/")
}
