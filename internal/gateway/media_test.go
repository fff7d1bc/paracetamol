package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"paracetamol/internal/catalog"
)

func mediaBody(content string) string {
	return `{"model":"fixture","messages":[{"role":"user","content":` + content + `}]}`
}

func imagePart(url string) string {
	encoded, _ := json.Marshal(url)
	return `[{"type":"image_url","image_url":{"url":` + string(encoded) + `}}]`
}

func setMediaModel(server *Server, vision bool) Model {
	model := server.registry.byID["fixture"]
	model.Vision = vision
	server.registry.byID[model.ID] = model
	server.registry.Models = []Model{model}
	return model
}

func TestMediaRejectsUnsafeContentBeforeScheduling(t *testing.T) {
	for _, test := range []struct {
		name, content string
		vision        bool
	}{
		{"text-only model", imagePart("data:image/png;base64,YQ=="), false},
		{"remote HTTP", imagePart("http://private.example/PRIVATE_MEDIA_URL"), true},
		{"remote HTTPS", imagePart("https://private.example/PRIVATE_MEDIA_URL"), true},
		{"local file", imagePart("file:///PRIVATE_MEDIA_FILE"), true},
		{"raw base64", imagePart("UFJJVkFURV9NRURJQV9CWVRFUw=="), true},
		{"empty URL", imagePart(""), true},
		{"empty bytes", imagePart("data:image/png;base64,"), true},
		{"missing image subtype", imagePart("data:image/;base64,YQ=="), true},
		{"not an image", imagePart("data:audio/wav;base64,YQ=="), true},
		{"not base64", imagePart("data:image/png,YQ=="), true},
		{"bad base64", imagePart("data:image/png;base64,PRIVATE%%%"), true},
		{"bad padding", imagePart("data:image/png;base64,YQ="), true},
		{"noncanonical padding bits", imagePart("data:image/png;base64,YR=="), true},
		{"missing image object", `[{"type":"image_url"}]`, true},
		{"null image object", `[{"type":"image_url","image_url":null}]`, true},
		{"string image object", `[{"type":"image_url","image_url":"PRIVATE_MEDIA"}]`, true},
		{"array image object", `[{"type":"image_url","image_url":[]}]`, true},
		{"missing object URL", `[{"type":"image_url","image_url":{}}]`, true},
		{"null object URL", `[{"type":"image_url","image_url":{"url":null}}]`, true},
		{"number object URL", `[{"type":"image_url","image_url":{"url":1}}]`, true},
		{"uppercase type cannot hide media", `[{"type":"image_url","TYPE":"text","image_url":{"url":"https://private.example/PRIVATE_MEDIA_URL"}}]`, true},
		{"uppercase URL cannot hide remote URL", `[{"type":"image_url","image_url":{"url":"https://private.example/PRIVATE_MEDIA_URL","URL":"data:image/png;base64,YQ=="}}]`, true},
		{"uppercase image object cannot hide remote URL", `[{"type":"image_url","image_url":{"url":"https://private.example/PRIVATE_MEDIA_URL"},"IMAGE_URL":{"url":"data:image/png;base64,YQ=="}}]`, true},
		{"audio alias", `[{"type":"input_audio","input_audio":{"url":"https://private.example/PRIVATE_MEDIA_URL"}}]`, true},
		{"video alias", `[{"type":"input_video","input_video":{"data":"https://private.example/PRIVATE_MEDIA_URL"}}]`, true},
		{"video URL alias", `[{"type":"video_url","video_url":{"url":"https://private.example/PRIVATE_MEDIA_URL"}}]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rejected media reached backend") }))
			defer upstream.Close()
			_, scheduler, handler := testGatewayServer(t, upstream)
			setMediaModel(handler, test.vision)
			var logs bytes.Buffer
			handler.log = &logs
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(mediaBody(test.content))))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_media"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			status, err := scheduler.Status(context.Background())
			if err != nil || status.State != StateUnloaded || status.ActiveRequests != 0 || status.QueuedRequests != 0 {
				t.Fatalf("rejected media scheduled work: %+v, %v", status, err)
			}
			records := handler.requests.Recent(1)
			if len(records) != 1 || records[0].Outcome != OutcomeRejected || records[0].HTTPStatus != http.StatusBadRequest {
				t.Fatalf("records=%+v", records)
			}
			encoded, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{response.Body.String(), logs.String(), string(encoded)} {
				for _, private := range []string{"PRIVATE", "private.example", "data:image/", "UFJJVkFURV9NRURJQV9CWVRFUw=="} {
					if strings.Contains(text, private) {
						t.Fatalf("media leaked into errors/logs/reports: %s", private)
					}
				}
			}
		})
	}
}

func TestMediaPreservesTextAndInlineImageRequests(t *testing.T) {
	for _, content := range []string{
		`"Text mentions image_url, file:///private and https://private.example without requesting media"`,
		`[{"type":"text","text":"image_url https://private.example"}]`,
		`[{"type":"text","text":"Describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YQ==","detail":"auto"}}]`,
		`[{"type":"image_url","image_url":{"url":"data:image/webp;base64,Yg=="}},{"type":"image_url","image_url":{"url":"data:image/custom+format;base64,Yw=="}}]`,
	} {
		for _, vision := range []bool{false, true} {
			if !vision && strings.Contains(content, `"type":"image_url"`) {
				continue
			}
			t.Run(content+"/vision="+map[bool]string{false: "false", true: "true"}[vision], func(t *testing.T) {
				seen := make(chan []byte, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					seen <- body
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"choices":[]}`)
				}))
				defer upstream.Close()
				_, _, handler := testGatewayServer(t, upstream)
				setMediaModel(handler, vision)
				// Opaque tool schemas, tool arguments and string contents must not
				// become media merely because they name the same protocol fields.
				body := strings.TrimSuffix(mediaBody(content), "}") + `,"custom":{"number":1.2300},"tools":[{"type":"function","function":{"name":"image_url","parameters":{"url":"https://private.example","type":"video_url"}}}],"opaque":{"type":"input_audio"}}`
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
				if response.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				if got := <-seen; string(got) != body {
					t.Fatal("media validation changed request bytes")
				}
			})
		}
	}
}

func TestMediaSamplingProvenanceAndPrivacy(t *testing.T) {
	const private = "PRIVATE_IMAGE_BYTES"
	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(private))
	body := strings.TrimSuffix(mediaBody(imagePart(image)), "}") + `,"temperature":0.25,"tools":[{"opaque":9007199254740993}]}`
	seen := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		seen <- data
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer upstream.Close()
	_, _, handler := testGatewayServer(t, upstream)
	model := setMediaModel(handler, true)
	model.SamplingPolicy = "media-fixture"
	model.samplingDefaults = catalog.SamplingPolicy{ID: "media-fixture", NonThinking: map[string]any{"temperature": 0.7, "top_p": 0.9}}
	handler.registry.byID[model.ID] = model
	var logs bytes.Buffer
	handler.log = &logs
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	forwarded := <-seen
	before, err := decodeJSONObject([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeJSONObject(forwarded)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"messages", "tools", "temperature"} {
		var original, sent bytes.Buffer
		if err := json.Compact(&original, before[key]); err != nil {
			t.Fatal(err)
		}
		if err := json.Compact(&sent, after[key]); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original.Bytes(), sent.Bytes()) {
			t.Fatalf("sampling changed media/unrelated field %s", key)
		}
	}
	if string(after["top_p"]) != "0.9" {
		t.Fatal("lost gateway sampler default")
	}
	records := handler.requests.Recent(1)
	if len(records) != 1 {
		t.Fatalf("records=%+v", records)
	}
	sources := map[string]ControlSource{}
	for _, control := range records[0].Controls.Sampling {
		sources[control.Name] = control.Source
	}
	if sources["temperature"] != ControlClient || sources["top_p"] != ControlGatewayDefault || records[0].Controls.SamplingPolicy != "media-fixture" {
		t.Fatalf("lost sampler provenance: %+v", records[0].Controls)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{logs.String(), string(encoded)} {
		if strings.Contains(text, image) || strings.Contains(text, private) {
			t.Fatal("image entered logs/reports")
		}
	}
}

func TestMediaDoesNotInterpretOpaqueToolArgumentsOrResults(t *testing.T) {
	body := []byte(`{"model":"fixture","messages":[{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"inspect","arguments":"{\"content\":[{\"type\":\"image_url\",\"image_url\":{\"url\":\"https://private.example\"}}]}"}}]},{"role":"tool","content":"{\"type\":\"video_url\",\"url\":\"https://private.example\"}"}]}`)
	if err := validateRequestMedia(body, Model{}); err != nil {
		t.Fatalf("opaque tool data became media: %v", err)
	}
}

func TestMediaUsesExactNativeMessageAndContentKeys(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://private.example/PRIVATE_MEDIA"}}]}],"MESSAGES":[]}`,
		`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://private.example/PRIVATE_MEDIA"}}],"CONTENT":"ordinary text"}]}`,
	} {
		if err := validateRequestMedia([]byte(body), Model{Vision: true}); err == nil || strings.Contains(err.Error(), "PRIVATE_MEDIA") || strings.Contains(err.Error(), "private.example") {
			t.Fatalf("native lowercase media was hidden or exposed by a case alias: %v", err)
		}
	}
}

func TestMediaAuthenticationAndWholeBodyLimitPrecedeValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rejected request reached backend") }))
	defer upstream.Close()
	_, scheduler, handler := testGatewayServer(t, upstream)
	setMediaModel(handler, true)
	handler.auth, _ = newBearerAuth(testAPIKey)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", unreadableAuthBody{}))
	if response.Code != http.StatusUnauthorized || len(handler.requests.Recent(1)) != 0 {
		t.Fatal("media processing preceded authentication")
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(mediaBody(imagePart("data:image/png;base64,"+strings.Repeat("A", MaxRequestBytes)))))
	request.Header.Set("Authorization", "Bearer "+testAPIKey)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("body limit lost: %d %s", response.Code, response.Body.String())
	}
	status, err := scheduler.Status(context.Background())
	if err != nil || status.State != StateUnloaded || status.ActiveRequests != 0 || status.QueuedRequests != 0 {
		t.Fatalf("rejected request scheduled work: %+v, %v", status, err)
	}
}
