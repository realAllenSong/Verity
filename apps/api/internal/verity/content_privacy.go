package verity

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// A deterministic baseline, not a promise of complete DLP. Organizations must
// extend this policy for their data. Raw storage is local to the deployment;
// all inspection surfaces receive the protected projection, including Raw.
var credentialPattern = regexp.MustCompile(`(?i)(?:bearer\s+[a-z0-9._~+/=-]{8,}|(?:sk-|ghp_|github_pat_|xox[baprs]-)[a-z0-9_-]{8,}|(?:password|api[_ -]?key|access[_ -]?token|client[_ -]?secret)\s*[:=]\s*["']?[^\s,"';}]{4,})`)
var secretKeys = map[string]bool{"password": true, "authorization": true, "api_key": true, "apikey": true, "access_token": true, "refresh_token": true, "client_secret": true, "secret": true, "credentials": true, "cookie": true, "set-cookie": true}

func protectText(text string) string {
	text = credentialPattern.ReplaceAllString(text, "[secret redacted]")
	text = secretPattern.ReplaceAllString(text, "[secret redacted]")
	text = emailPattern.ReplaceAllString(text, "[email redacted]")
	return phonePattern.ReplaceAllString(text, "[phone redacted]")
}

func privateObject(value map[string]any) bool {
	metadata, _ := value["metadata"].(map[string]any)
	return asBool(value["sensitive_only"]) || asBool(metadata["sensitive_only"]) || literal(value, "visibility") == "private"
}

func protectValue(value any) any {
	switch v := value.(type) {
	case string:
		return protectText(v)
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = protectValue(child)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		if privateObject(v) {
			// Preserve only structural linkage for an explicitly private fragment.
			for _, key := range []string{"record_id", "event_id", "id", "dataset_id", "batch_id", "role", "type", "source_path", "reply_to", "synthetic"} {
				if child, ok := v[key]; ok {
					out[key] = protectValue(child)
				}
			}
			out["content"] = "[Private content withheld]"
			out["visibility"] = "private"
			return out
		}
		for key, child := range v {
			if secretKeys[strings.ToLower(key)] {
				out[key] = "[secret redacted]"
			} else {
				out[key] = protectValue(child)
			}
		}
		return out
	default:
		return value
	}
}

func protectedRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{"content":"[Unreadable content withheld]"}`)
	}
	data, _ := json.Marshal(protectValue(value))
	return data
}

func writeProtectedJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "Cannot serialize inspection", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(protectedRaw(data))
	_, _ = w.Write([]byte{'\n'})
}
