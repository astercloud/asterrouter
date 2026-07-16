package gatewaycore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExtractCredentialUsesOneProtocolApprovedTransport(t *testing.T) {
	tests := []struct {
		name       string
		protocol   Protocol
		headers    http.Header
		wantToken  string
		wantSigned string
		transport  string
	}{
		{name: "bearer", protocol: ProtocolOpenAIChat, headers: http.Header{"Authorization": []string{"Bearer key-1"}}, wantToken: "key-1", transport: "authorization_bearer"},
		{name: "signed context", protocol: ProtocolOpenAIChat, headers: http.Header{"Authorization": []string{"Aster-Context signed-1"}}, wantSigned: "signed-1", transport: "authorization_aster_context"},
		{name: "anthropic", protocol: ProtocolAnthropicMessages, headers: http.Header{"X-Api-Key": []string{"key-2"}}, wantToken: "key-2", transport: "anthropic_x_api_key"},
		{name: "gemini", protocol: ProtocolGeminiGenerate, headers: http.Header{"X-Goog-Api-Key": []string{"key-3"}}, wantToken: "key-3", transport: "gemini_x_goog_api_key"},
		{name: "realtime browser", protocol: ProtocolRealtime, headers: http.Header{"Sec-Websocket-Protocol": []string{"realtime, openai-insecure-api-key.key-4"}}, wantToken: "key-4", transport: "realtime_subprotocol"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &http.Request{Header: test.headers, URL: &url.URL{}}
			got, err := ExtractCredential(req, test.protocol)
			if err != nil {
				t.Fatalf("ExtractCredential(): %v", err)
			}
			if got.BearerToken != test.wantToken || got.SignedContext != test.wantSigned || got.Transport != test.transport {
				t.Fatalf("credential = %+v", got)
			}
		})
	}
}

func TestCanonicalizeNativeTextProtocolsPreservesProviderPayload(t *testing.T) {
	header := http.Header{"X-Request-ID": []string{"native-protocol"}, "Idempotency-Key": []string{"native-idem"}}

	responsesRaw := []byte(`{"model":"responses-model","input":"hello","metadata":{"opaque":"kept"},"stream":true}`)
	responses, err := CanonicalizeOpenAIResponses(responsesRaw, header)
	if err != nil || responses.Protocol != ProtocolOpenAIResponses || !responses.Stream || string(responses.Payload) != string(responsesRaw) || responses.Operation != "chat_completion" {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
	anthropicRaw := []byte(`{"model":"claude-model","max_tokens":64,"system":"keep","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	anthropic, err := CanonicalizeAnthropicMessages(anthropicRaw, header)
	if err != nil || anthropic.Protocol != ProtocolAnthropicMessages || anthropic.MessageCount != 1 || !anthropic.Stream || string(anthropic.Payload) != string(anthropicRaw) {
		t.Fatalf("anthropic=%+v err=%v", anthropic, err)
	}
	geminiRaw := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":64}}`)
	gemini, err := CanonicalizeGeminiGenerate(geminiRaw, header, "gemini-model", true)
	if err != nil || gemini.Protocol != ProtocolGeminiGenerate || !gemini.Stream || gemini.Model != "gemini-model" || string(gemini.Payload) != string(geminiRaw) {
		t.Fatalf("gemini=%+v err=%v", gemini, err)
	}
}

func TestExtractCredentialFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		rawURL   string
		headers  http.Header
		want     error
	}{
		{name: "missing", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", want: ErrCredentialMissing},
		{name: "query key", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions?api_key=secret", want: ErrQueryCredentialRejected},
		{name: "multiple", protocol: ProtocolAnthropicMessages, rawURL: "https://gateway.test/v1/messages", headers: http.Header{"Authorization": []string{"Bearer one"}, "X-Api-Key": []string{"two"}}, want: ErrCredentialConflict},
		{name: "anthropic header on openai", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", headers: http.Header{"X-Api-Key": []string{"one"}}, want: ErrCredentialTransportRejected},
		{name: "unknown scheme", protocol: ProtocolOpenAIChat, rawURL: "https://gateway.test/v1/chat/completions", headers: http.Header{"Authorization": []string{"Basic abc"}}, want: ErrCredentialTransportRejected},
		{name: "realtime conflicting", protocol: ProtocolRealtime, rawURL: "https://gateway.test/v1/realtime?model=audio", headers: http.Header{"Authorization": []string{"Bearer one"}, "Sec-Websocket-Protocol": []string{"realtime, openai-insecure-api-key.two"}}, want: ErrCredentialConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, test.rawURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header = test.headers
			_, err = ExtractCredential(req, test.protocol)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCanonicalizeOpenAIChatProducesStableSafeEnvelope(t *testing.T) {
	raw := []byte(`{"model":"public-model","messages":[{"role":"user","content":"synthetic"}],"stream":true,"user":"subject-1"}`)
	header := http.Header{"X-Request-Id": []string{"request-1"}, "Idempotency-Key": []string{"idem-1"}}
	got, err := CanonicalizeOpenAIChat(raw, header)
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIChat(): %v", err)
	}
	if got.ID != "op_request-1" || got.Protocol != ProtocolOpenAIChat || got.Lane != LaneDirect || got.Model != "public-model" || !got.Stream || got.MessageCount != 1 || got.IdempotencyKey != "idem-1" || got.StickyKey != "subject-1" {
		t.Fatalf("canonical request = %+v", got)
	}
	if len(got.Fingerprint) != 64 || !strings.Contains(string(got.Payload), "synthetic") {
		t.Fatalf("canonical payload or fingerprint is invalid: %+v", got)
	}
	raw[0] = '['
	if got.Payload[0] != '{' {
		t.Fatal("canonical request retained a mutable caller buffer")
	}
}

func TestCanonicalizeOpenAIModelsProducesCanonicalEnvelope(t *testing.T) {
	got, err := CanonicalizeOpenAIModels(http.Header{"X-Request-Id": []string{"models-1"}})
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIModels(): %v", err)
	}
	if got.ID != "op_models-1" || got.Protocol != ProtocolOpenAIModels || got.Operation != "list_models" || got.Modality != "metadata" || got.Lane != LaneDirect || got.Model != "" || len(got.Fingerprint) != 64 {
		t.Fatalf("canonical models request = %+v", got)
	}
}

func TestCanonicalizeRealtimeSessionFreezesAdmissionEnvelope(t *testing.T) {
	header := http.Header{
		"X-Request-Id":                              []string{"realtime-1"},
		"Idempotency-Key":                           []string{"realtime-idem-1"},
		"X-Asterrouter-Sticky-Key":                  []string{"conversation-1"},
		"X-Asterrouter-Estimated-Audio-Duration-Ms": []string{"90000"},
	}
	got, err := CanonicalizeRealtimeSession(header, " public-realtime ")
	if err != nil {
		t.Fatalf("CanonicalizeRealtimeSession(): %v", err)
	}
	if got.ID != "op_realtime-1" || got.Protocol != ProtocolRealtime || got.Operation != "realtime_session" || got.Modality != "audio" || got.Lane != LaneDirect || got.Model != "public-realtime" || !got.Stream || got.IdempotencyKey != "realtime-idem-1" || got.StickyKey != "conversation-1" || got.AudioDurationMS != 90000 || got.ResponseMode != "stream" {
		t.Fatalf("canonical realtime request = %+v", got)
	}
	if len(got.Fingerprint) != 64 || strings.Contains(string(got.Payload), "realtime-idem-1") {
		t.Fatalf("realtime fingerprint payload is invalid: %+v", got)
	}
}

func TestCanonicalizeRealtimeSessionRejectsUnsafeAdmission(t *testing.T) {
	valid := http.Header{"Idempotency-Key": []string{"realtime-idem"}}
	if _, err := CanonicalizeRealtimeSession(valid, " "); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("missing model error=%v", err)
	}
	if _, err := CanonicalizeRealtimeSession(http.Header{}, "model"); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("missing idempotency error=%v", err)
	}
	tooLong := valid.Clone()
	tooLong.Set("X-AsterRouter-Estimated-Audio-Duration-Ms", "3600001")
	if _, err := CanonicalizeRealtimeSession(tooLong, "model"); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("duration error=%v", err)
	}
}

func TestCanonicalizeOpenAIImageGenerationFreezesInteractionContract(t *testing.T) {
	header := http.Header{"X-Request-Id": []string{"image-1"}, "Idempotency-Key": []string{"image-idem-1"}}
	first, err := CanonicalizeOpenAIImageGeneration([]byte(`{"model":"image-model","prompt":" synthetic ","n":2,"stream":true,"delivery_mode":"artifact"}`), header)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalizeOpenAIImageGeneration([]byte(`{"delivery_mode":"artifact","response_mode":"stream","prompt":"synthetic","n":2,"model":"image-model"}`), header)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "op_image-1" || first.Protocol != ProtocolOpenAIImages || first.Lane != LaneDirect || !first.Stream ||
		first.ResponseMode != "stream" || first.PreviewMode != "none" || first.DeliveryMode != "artifact" || first.OutputCount != 2 ||
		first.Fingerprint != second.Fingerprint || string(first.Payload) != string(second.Payload) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var canonical struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(first.Payload, &canonical); err != nil || canonical.Input["stream"] != nil || canonical.Input["delivery_mode"] != nil {
		t.Fatalf("provider input retained transport controls: %s err=%v", first.Payload, err)
	}
}

func TestCanonicalizeOpenAIImageGenerationRejectsUnsafeContracts(t *testing.T) {
	validHeader := http.Header{"Idempotency-Key": []string{"image-idem"}}
	tests := []struct {
		body   string
		header http.Header
	}{
		{body: `{"model":"image-model","prompt":"synthetic"}`},
		{body: `{"model":"image-model","prompt":"synthetic","n":0}`, header: validHeader},
		{body: `{"model":"image-model","prompt":"synthetic","response_mode":"blocking","preview_mode":"required"}`, header: validHeader},
		{body: `{"model":"image-model","prompt":"synthetic","delivery_mode":"unknown"}`, header: validHeader},
	}
	for _, test := range tests {
		if _, err := CanonicalizeOpenAIImageGeneration([]byte(test.body), test.header); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("body=%s error=%v", test.body, err)
		}
	}
}

func TestCanonicalizeDurableJobNormalizesPayloadForIdempotency(t *testing.T) {
	header := http.Header{"X-Client-Request-Id": []string{"job-request-1"}, "Idempotency-Key": []string{"job-idem-1"}}
	first, err := CanonicalizeDurableJob([]byte(`{"model":"image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic","count":1}}`), header)
	if err != nil {
		t.Fatalf("CanonicalizeDurableJob(first): %v", err)
	}
	second, err := CanonicalizeDurableJob([]byte(`{
  "input": {"count": 1, "prompt": "synthetic"},
  "modality": "IMAGE",
  "operation": "IMAGE_GENERATION",
  "model": "image-model"
}`), header)
	if err != nil {
		t.Fatalf("CanonicalizeDurableJob(second): %v", err)
	}
	if first.ID != "op_job-request-1" || first.Protocol != ProtocolAsterJobs || first.Lane != LaneDurable || first.Operation != "image_generation" || first.Modality != "image" || first.IdempotencyKey != "job-idem-1" {
		t.Fatalf("canonical job request = %+v", first)
	}
	if first.Fingerprint != second.Fingerprint || string(first.Payload) != string(second.Payload) {
		t.Fatalf("normalized payloads differ first=%s second=%s", first.Payload, second.Payload)
	}
}

func TestCanonicalizeDurableJobCapturesMediaUsageEstimate(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		images  int
		videoMS int64
		audioMS int64
	}{
		{name: "images", body: `{"model":"image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic","n":3,"count":3}}`, images: 3},
		{name: "video milliseconds", body: `{"model":"video-model","operation":"video_generation","modality":"video","input":{"prompt":"synthetic","duration_ms":1250}}`, images: 1, videoMS: 1250},
		{name: "video seconds", body: `{"model":"video-model","operation":"video_generation","modality":"video","input":{"prompt":"synthetic","duration_seconds":1.25}}`, images: 1, videoMS: 1250},
		{name: "audio", body: `{"model":"audio-model","operation":"audio_generation","modality":"audio","input":{"prompt":"synthetic","duration_seconds":2}}`, images: 1, audioMS: 2000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := CanonicalizeDurableJob([]byte(test.body), http.Header{"Idempotency-Key": []string{"media-idem"}})
			if err != nil {
				t.Fatal(err)
			}
			if request.OutputCount != test.images || request.VideoDurationMS != test.videoMS || request.AudioDurationMS != test.audioMS {
				t.Fatalf("request=%+v", request)
			}
		})
	}
}

func TestCanonicalizeDurableJobRejectsInvalidMediaUsageEstimate(t *testing.T) {
	bodies := []string{
		`{"model":"image-model","operation":"image_generation","modality":"image","input":{"n":1,"count":2}}`,
		`{"model":"video-model","operation":"video_generation","modality":"video","input":{"duration_ms":"1000"}}`,
		`{"model":"video-model","operation":"video_generation","modality":"video","input":{"duration_ms":1000.5}}`,
		`{"model":"video-model","operation":"video_generation","modality":"video","input":{"duration_ms":1000,"duration_seconds":2}}`,
		`{"model":"audio-model","operation":"audio_generation","modality":"audio","input":{"duration_seconds":0}}`,
	}
	for _, body := range bodies {
		if _, err := CanonicalizeDurableJob([]byte(body), http.Header{"Idempotency-Key": []string{"media-idem"}}); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("body=%s error=%v", body, err)
		}
	}
}

func TestCanonicalizeDurableJobRejectsInvalidPayloadAndConflictingRequestIDs(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		header http.Header
	}{
		{name: "missing input", body: `{"model":"image-model","operation":"image_generation","modality":"image"}`},
		{name: "null input", body: `{"model":"image-model","operation":"image_generation","modality":"image","input":null}`},
		{name: "invalid operation", body: `{"model":"image-model","operation":"Image Generate","modality":"image","input":{}}`},
		{name: "conflicting request ids", body: `{"model":"image-model","operation":"image_generation","modality":"image","input":{}}`, header: http.Header{"X-Request-Id": []string{"one"}, "X-Client-Request-Id": []string{"two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CanonicalizeDurableJob([]byte(test.body), test.header); !errors.Is(err, ErrInvalidCanonicalRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCanonicalizeAIJobActionFreezesSourceAndIdempotencyContract(t *testing.T) {
	header := http.Header{"X-Request-Id": []string{"action-request-1"}, "Idempotency-Key": []string{"action-idem-1"}}
	request, err := CanonicalizeAIJobAction([]byte(`{"action":"remix","input":{"prompt":"synthetic"}}`), header, "job_source_1", "video-model", "video_generation", "video")
	if err != nil {
		t.Fatalf("CanonicalizeAIJobAction(): %v", err)
	}
	if request.Protocol != ProtocolAsterJobs || request.Lane != LaneDurable || request.Operation != "video_generation" || request.Modality != "video" || request.ResponseMode != "async" || request.DeliveryMode != "artifact" || request.IdempotencyKey != "action-idem-1" || request.VideoDurationMS != 0 {
		t.Fatalf("request=%+v", request)
	}
	if !strings.Contains(string(request.Payload), `"action":"remix"`) || !strings.Contains(string(request.Payload), `"source_job_id":"job_source_1"`) {
		t.Fatalf("payload=%s", request.Payload)
	}
	for _, raw := range []string{
		`{"action":"unknown","input":{"prompt":"synthetic"}}`,
		`{"action":"remix"}`,
	} {
		if _, err := CanonicalizeAIJobAction([]byte(raw), header, "job_source_1", "video-model", "video_generation", "video"); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("raw=%s error=%v", raw, err)
		}
	}
}

func TestCanonicalizeOpenAIMediaJobUsesDurableContract(t *testing.T) {
	header := http.Header{"X-Request-Id": []string{"video-request-1"}, "Idempotency-Key": []string{"video-idem-1"}}
	request, err := CanonicalizeOpenAIMediaJob([]byte(`{"model":"video-model","prompt":"synthetic","duration_seconds":1.5}`), header, "video", "video_generation")
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIMediaJob(): %v", err)
	}
	if request.Protocol != ProtocolOpenAIMedia || request.Lane != LaneDurable || request.Modality != "video" || request.Operation != "video_generation" || request.VideoDurationMS != 1500 || request.IdempotencyKey != "video-idem-1" {
		t.Fatalf("request=%+v", request)
	}
	if request.ResponseMode != "async" || request.DeliveryMode != "artifact" || request.Stream {
		t.Fatalf("async interaction contract=%+v", request)
	}
	if !strings.Contains(string(request.Payload), `"input":{"duration_seconds":1.5,"prompt":"synthetic"}`) || !strings.Contains(string(request.Payload), `"response_mode":"async"`) {
		t.Fatalf("canonical payload=%s", request.Payload)
	}
}

func TestCanonicalizeOpenAIMediaJobSupportsExplicitInteractionModes(t *testing.T) {
	header := http.Header{"Idempotency-Key": []string{"media-idem"}}
	tests := []struct {
		name         string
		body         string
		wantLane     Lane
		wantMode     string
		wantDelivery string
		wantStream   bool
	}{
		{name: "async default", body: `{"model":"video-model","prompt":"synthetic","duration_seconds":1}`, wantLane: LaneDurable, wantMode: "async", wantDelivery: "artifact"},
		{name: "blocking", body: `{"model":"video-model","prompt":"synthetic","duration_seconds":1,"response_mode":"blocking"}`, wantLane: LaneDirect, wantMode: "blocking", wantDelivery: "inline"},
		{name: "stream", body: `{"model":"video-model","prompt":"synthetic","duration_seconds":1,"response_mode":"stream","stream":true,"delivery_mode":"artifact"}`, wantLane: LaneDirect, wantMode: "stream", wantDelivery: "artifact", wantStream: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := CanonicalizeOpenAIMediaJob([]byte(test.body), header, "video", "video_generation")
			if err != nil {
				t.Fatalf("error=%v", err)
			}
			if request.Protocol != ProtocolOpenAIMedia || request.Lane != test.wantLane || request.ResponseMode != test.wantMode || request.DeliveryMode != test.wantDelivery || request.Stream != test.wantStream {
				t.Fatalf("request=%+v", request)
			}
		})
	}
	for _, test := range []string{
		`{"model":"video-model","prompt":"synthetic","stream":"false"}`,
		`{"model":"video-model","prompt":"synthetic","response_mode":"blocking","stream":true}`,
		`{"model":"video-model","prompt":"synthetic","response_mode":"async","stream":true}`,
		`{"model":"video-model","prompt":"synthetic","response_mode":"async","delivery_mode":"inline"}`,
		`{"model":"video-model"}`,
	} {
		if _, err := CanonicalizeOpenAIMediaJob([]byte(test), header, "video", "video_generation"); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("body=%s error=%v", test, err)
		}
	}
	if _, err := CanonicalizeOpenAIMediaJob([]byte(`{"model":"video-model","prompt":"synthetic"}`), header, "image", "video_generation"); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("wrong modality error=%v", err)
	}
}

func TestCanonicalizeOpenAIMediaJobPreservesPreviewContract(t *testing.T) {
	header := http.Header{"Idempotency-Key": []string{"media-preview-idem"}}
	request, err := CanonicalizeOpenAIMediaJob([]byte(`{"model":"video-model","prompt":"synthetic","response_mode":"stream","stream":true,"preview_mode":"preferred","delivery_mode":"artifact"}`), header, "video", "video_generation")
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIMediaJob(): %v", err)
	}
	if request.PreviewMode != "preferred" || !strings.Contains(string(request.Payload), `"preview_mode":"preferred"`) {
		t.Fatalf("preview contract was not preserved: request=%+v payload=%s", request, request.Payload)
	}
	for _, body := range []string{
		`{"model":"video-model","prompt":"synthetic","response_mode":"blocking","preview_mode":"required"}`,
		`{"model":"video-model","prompt":"synthetic","response_mode":"async","preview_mode":"required"}`,
		`{"model":"video-model","prompt":"synthetic","preview_mode":"invalid"}`,
	} {
		if _, err := CanonicalizeOpenAIMediaJob([]byte(body), header, "video", "video_generation"); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("body=%s error=%v, want invalid canonical request", body, err)
		}
	}
}

func TestCanonicalizeOpenAIAudioFileUsesDigestForStableIdentity(t *testing.T) {
	header := http.Header{"X-Request-Id": []string{"audio-request-1"}, "Idempotency-Key": []string{"audio-idem-1"}}
	content := []byte{0, 1, 2, 3, 255}
	digest := sha256.Sum256(content)
	input := CanonicalInputArtifact{Filename: "first.wav", MediaType: "audio/wav", SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Content: content}
	first, err := CanonicalizeOpenAIAudioFile(map[string][]string{
		"model": {"public-audio"}, "language": {"en"}, "stream": {"true"},
	}, input, []byte("multipart-one"), "multipart/form-data; boundary=one", header, ProtocolOpenAIAudioTranscriptions)
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIAudioFile(): %v", err)
	}
	input.Filename = "renamed.wav"
	second, err := CanonicalizeOpenAIAudioFile(map[string][]string{
		"stream": {"true"}, "language": {"en"}, "model": {"public-audio"},
	}, input, []byte("multipart-two"), "multipart/form-data; boundary=two", header, ProtocolOpenAIAudioTranscriptions)
	if err != nil {
		t.Fatalf("CanonicalizeOpenAIAudioFile() second: %v", err)
	}
	if first.Fingerprint != second.Fingerprint || first.Protocol != ProtocolOpenAIAudioTranscriptions || first.Operation != "audio_transcription" || first.Lane != LaneDirect || !first.Stream || first.ResponseMode != "stream" {
		t.Fatalf("canonical audio requests differ: first=%+v second=%+v", first, second)
	}
	if bytes.Contains(first.Payload, content) || bytes.Contains(first.Payload, []byte("first.wav")) || first.InputArtifact == nil || !bytes.Equal(first.InputArtifact.Content, content) {
		t.Fatalf("audio bytes leaked into canonical payload: %s", first.Payload)
	}
	if _, err := CanonicalizeOpenAIAudioFile(map[string][]string{"model": {"one", "two"}}, input, []byte("multipart"), "multipart/form-data; boundary=one", header, ProtocolOpenAIAudioTranscriptions); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("duplicate model error=%v", err)
	}
}

func TestCanonicalizeOpenAIAudioSpeechPreservesBinaryAndSSEContracts(t *testing.T) {
	header := http.Header{}
	header.Set("X-AsterRouter-Estimated-Audio-Duration-Ms", "1250")
	header.Set("Idempotency-Key", "speech-blocking")
	blocking, err := CanonicalizeOpenAIAudioSpeech([]byte(`{"model":"voice-model","input":"hello","voice":"alloy","response_format":"mp3"}`), header)
	if err != nil || blocking.Protocol != ProtocolOpenAIAudioSpeech || blocking.Operation != "speech_generation" || blocking.Stream || blocking.ResponseMode != "blocking" || blocking.InputCharacters != 5 || blocking.AudioDurationMS != 1250 {
		t.Fatalf("blocking=%+v err=%v", blocking, err)
	}
	streamHeader := http.Header{}
	streamHeader.Set("Idempotency-Key", "speech-stream")
	streaming, err := CanonicalizeOpenAIAudioSpeech([]byte(`{"model":"voice-model","input":"hello","voice":"alloy","stream_format":"sse"}`), streamHeader)
	if err != nil || !streaming.Stream || streaming.ResponseMode != "stream" {
		t.Fatalf("streaming=%+v err=%v", streaming, err)
	}
	for _, raw := range []string{
		`{"input":"hello","voice":"alloy"}`,
		`{"model":"voice-model","voice":"alloy"}`,
		`{"model":"voice-model","input":"hello","voice":"alloy","stream_format":"invalid"}`,
	} {
		if _, err := CanonicalizeOpenAIAudioSpeech([]byte(raw), http.Header{}); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("raw=%s error=%v", raw, err)
		}
	}
	invalidHeader := http.Header{}
	invalidHeader.Set("X-AsterRouter-Estimated-Audio-Duration-Ms", "invalid")
	if _, err := CanonicalizeOpenAIAudioSpeech([]byte(`{"model":"voice-model","input":"hello","voice":"alloy"}`), invalidHeader); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("invalid duration error=%v", err)
	}
}

func TestCanonicalizeOpenAIChatRejectsInvalidInput(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`[]`), []byte(`{"messages":[]}`), []byte(`{"model":`)} {
		if _, err := CanonicalizeOpenAIChat(raw, http.Header{}); !errors.Is(err, ErrInvalidCanonicalRequest) {
			t.Fatalf("payload %q error = %v", raw, err)
		}
	}
	if _, err := CanonicalizeOpenAIChat([]byte(`{"model":"model-a"}`), http.Header{"X-Request-Id": []string{"request\nforged"}}); !errors.Is(err, ErrInvalidCanonicalRequest) {
		t.Fatalf("unsafe request id error = %v", err)
	}
}
