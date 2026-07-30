package unifi

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxResponseBytes = 128 * 1024
	base64MinLen     = 256
)

var looksBase64 = regexp.MustCompile(`^[A-Za-z0-9+/\-_\r\n]+=*$`)

var sensitiveAssignmentRe = regexp.MustCompile(`(?i)(\b(?:authorization|password|passwd|pwd|token|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|client[_-]?secret|private[_-]?key|session[_-]?token)\b\s*[:=]\s*)(["']?)[^\s,"']+`)
var sensitiveJSONRe = regexp.MustCompile(`(?i)("(?:authorization|password|passwd|pwd|token|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|client[_-]?secret|private[_-]?key|session[_-]?token)"\s*:\s*")([^"]*)(")`)
var authorizationLineRe = regexp.MustCompile(`(?im)(\bauthorization\b\s*[:=]\s*).+$`)
var inlineBase64Re = regexp.MustCompile(`(?m)^(\s*\S+:\s*)([A-Za-z0-9+/\-_]{256,}={0,2})\s*$`)

// SanitizeForOutput recursively redacts sensitive values from arbitrary
// JSON-like output before it is returned to an agent.
func SanitizeForOutput(v interface{}) interface{} {
	return sanitizeValue("", v)
}

func sanitizeValue(key string, v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = sanitizeValue(k, item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, item := range val {
			out = append(out, sanitizeValue("", item))
		}
		return out
	case string:
		if isSensitiveKey(key) {
			return fmt.Sprintf("[REDACTED %d bytes]", len(val))
		}
		if replaced, ok := maybeTruncateBase64(val); ok {
			return replaced
		}
		return SanitizeText(val)
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	switch normalized {
	case "authorization", "password", "passwd", "pwd", "token", "apikey", "accesskey",
		"secret", "secretkey", "clientsecret", "privatekey", "sessiontoken":
		return true
	default:
		return false
	}
}

// SanitizeText redacts sensitive assignments and oversized inline blobs in raw
// text, primarily for API error bodies.
func SanitizeText(s string) string {
	s = sensitiveJSONRe.ReplaceAllString(s, `${1}[REDACTED]${3}`)
	s = authorizationLineRe.ReplaceAllString(s, `${1}[REDACTED]`)
	s = sensitiveAssignmentRe.ReplaceAllString(s, `${1}${2}[REDACTED]`)
	s = inlineBase64Re.ReplaceAllStringFunc(s, func(match string) string {
		parts := inlineBase64Re.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return fmt.Sprintf("%s[BASE64_DATA %d chars]", parts[1], len(parts[2]))
	})
	if len(s) > maxResponseBytes {
		total := len(s)
		s = s[:maxResponseBytes] + fmt.Sprintf("\n[OUTPUT TRUNCATED - %d bytes total, showing first %d]", total, maxResponseBytes)
	}
	return s
}

func maybeTruncateBase64(s string) (string, bool) {
	if len(s) < base64MinLen || looksLikeText(s) {
		return "", false
	}

	trimmed := strings.TrimSpace(s)
	if !looksBase64.MatchString(trimmed) {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(trimmed)
		if err != nil {
			return "", false
		}
	}

	return fmt.Sprintf("[BASE64_DATA %d bytes decoded]", len(decoded)), true
}

func looksLikeText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	sample := s
	if len(sample) > 512 {
		sample = sample[:512]
	}
	textChars := 0
	for _, r := range sample {
		if r == ' ' || r == '\t' || r == ',' || r == '.' || r == ':' || r == '{' || r == '}' || r == '"' {
			textChars++
		}
	}
	return float64(textChars)/float64(len(sample)) > 0.05
}

// TruncateJSON rejects oversized serialized JSON with a compact, valid JSON
// error payload so clients do not receive partial JSON.
func TruncateJSON(data []byte) []byte {
	if len(data) <= maxResponseBytes {
		return data
	}
	msg := fmt.Sprintf(`{"error":"response too large","totalBytes":%d,"maxBytes":%d,"hint":"Use offset, limit, or filter to reduce output size"}`, len(data), maxResponseBytes)
	return []byte(msg)
}
