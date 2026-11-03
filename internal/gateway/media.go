package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// validateRequestMedia constrains actual Chat Completions content parts, not
// strings, tool arguments or schemas that happen to describe those parts.
// The native server also accepts remote media URLs; never forward those through
// this gateway. Validation does not rewrite any request bytes.
func validateRequestMedia(body []byte, model Model) error {
	object, err := decodeJSONObject(body)
	if err != nil {
		return fmt.Errorf("Cannot validate request media")
	}
	var messages []json.RawMessage
	if json.Unmarshal(object["messages"], &messages) != nil {
		// Overall message-shape validation remains the backend's responsibility.
		return nil
	}
	for _, rawMessage := range messages {
		var message map[string]json.RawMessage
		if json.Unmarshal(rawMessage, &message) != nil {
			continue
		}
		var parts []json.RawMessage
		if json.Unmarshal(message["content"], &parts) != nil {
			continue
		}
		for _, rawPart := range parts {
			// Native JSON keys are case-sensitive. Struct decoding would let a
			// later TYPE or URL alias hide the actual lowercase media fields.
			var part map[string]json.RawMessage
			if json.Unmarshal(rawPart, &part) != nil {
				continue
			}
			var kind string
			if json.Unmarshal(part["type"], &kind) != nil {
				continue
			}
			switch kind {
			case "image_url":
				if !model.Vision {
					return fmt.Errorf("The selected model does not support image input")
				}
				var image map[string]json.RawMessage
				var url string
				if json.Unmarshal(part["image_url"], &image) != nil || json.Unmarshal(image["url"], &url) != nil || !validInlineImage(url) {
					return fmt.Errorf("Image input requires a non-empty inline base64 image data URL")
				}
			case "input_audio", "input_video", "video_url":
				return fmt.Errorf("Audio and video input are not supported")
			}
		}
	}
	return nil
}

func validInlineImage(url string) bool {
	header, payload, ok := strings.Cut(url, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
		return false
	}
	if strings.TrimSuffix(strings.TrimPrefix(header, "data:image/"), ";base64") == "" {
		return false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	return err == nil && len(decoded) > 0
}
