package k8s

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxResponseBytes is the maximum size of a JSON-serialized tool response
// before it gets truncated. 128KB is well within LLM context limits while
// still allowing substantial resource output.
const maxResponseBytes = 128 * 1024

// base64MinLen is the minimum length of a string before we test whether it
// looks like base64.  Short strings are cheap and unlikely to be binary blobs.
const base64MinLen = 256

// looksBase64 matches a string that is purely base64 characters (with optional
// padding) and at least base64MinLen long.
var looksBase64 = regexp.MustCompile(`^[A-Za-z0-9+/\-_\r\n]+=*$`)

var sensitiveAssignmentRe = regexp.MustCompile(`(?i)(\b(?:authorization|password|passwd|pwd|token|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|client[_-]?secret|private[_-]?key|session[_-]?token)\b\s*[:=]\s*)(["']?)[^\s,"']+`)
var sensitiveJSONRe = regexp.MustCompile(`(?i)("(?:authorization|password|passwd|pwd|token|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|client[_-]?secret|private[_-]?key|session[_-]?token)"\s*:\s*")([^"]*)(")`)
var authorizationLineRe = regexp.MustCompile(`(?im)(\bauthorization\b\s*[:=]\s*).+$`)

// SanitizeResource redacts sensitive and bloated fields from a Kubernetes
// resource represented as unstructured content.  It:
//   - Replaces all values in Secret `data` and `stringData` fields with [REDACTED]
//   - Replaces ConfigMap `binaryData` values with a size placeholder
//   - Strips the `last-applied-configuration` annotation (duplicate of the resource, often contains secret data)
//   - Strips `managedFields` (verbose, not useful for LLM reasoning)
//   - Walks the entire tree and replaces any large base64-looking strings with a size placeholder
//   - Preserves key names so the LLM knows which fields exist
func SanitizeResource(content map[string]interface{}) map[string]interface{} {
	if content == nil {
		return nil
	}

	kind, _ := content["kind"].(string)

	// --- Kind-specific redactions ---

	if kind == "Secret" {
		redactMapValues(content, "data")
		redactMapValues(content, "stringData")
	}

	if kind == "ConfigMap" {
		redactMapValues(content, "binaryData")
	}

	// --- Metadata cleanup (applies to every resource) ---

	if metadata, ok := content["metadata"].(map[string]interface{}); ok {
		delete(metadata, "managedFields")
		delete(metadata, "selfLink")

		// Remove last-applied-configuration — it's a full JSON duplicate of
		// the resource spec and regularly contains base64 secret data.
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			// If annotations is now empty, remove it entirely.
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}
	}

	// --- Deep walk: replace any large base64 blobs anywhere in the tree ---
	deepSanitizeBase64(content)

	return content
}

// SanitizeForOutput recursively redacts sensitive values from arbitrary
// JSON-like output before it is returned to an agent.
func SanitizeForOutput(v interface{}) interface{} {
	return sanitizeValue("", v)
}

func sanitizeValue(key string, v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		if kind, _ := val["kind"].(string); kind == "Secret" {
			SanitizeResource(val)
		}
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

// redactMapValues replaces all values in content[field] with a redacted placeholder.
func redactMapValues(content map[string]interface{}, field string) {
	dataMap, ok := content[field].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range dataMap {
		if s, ok := v.(string); ok {
			dataMap[k] = fmt.Sprintf("[REDACTED %d bytes]", len(s))
		} else {
			dataMap[k] = "[REDACTED]"
		}
	}
}

// deepSanitizeBase64 recursively walks a map and replaces any string value
// that looks like a large base64 blob with a placeholder.  This catches
// embedded certificates, keys, and binary data in CRDs and annotations that
// are not covered by the kind-specific rules above.
func deepSanitizeBase64(obj map[string]interface{}) {
	for k, v := range obj {
		switch val := v.(type) {
		case string:
			if replaced, ok := maybeTruncateBase64(val); ok {
				obj[k] = replaced
			}
		case map[string]interface{}:
			deepSanitizeBase64(val)
		case []interface{}:
			deepSanitizeSlice(val)
		}
	}
}

func deepSanitizeSlice(arr []interface{}) {
	for i, v := range arr {
		switch val := v.(type) {
		case string:
			if replaced, ok := maybeTruncateBase64(val); ok {
				arr[i] = replaced
			}
		case map[string]interface{}:
			deepSanitizeBase64(val)
		case []interface{}:
			deepSanitizeSlice(val)
		}
	}
}

// maybeTruncateBase64 checks if s looks like a large base64-encoded blob and
// returns a placeholder if so.  It only fires on strings >= base64MinLen that
// consist entirely of base64 characters and do not look like readable text.
func maybeTruncateBase64(s string) (string, bool) {
	if len(s) < base64MinLen {
		return "", false
	}

	// Quick heuristic: if the string is valid UTF-8 with lots of spaces,
	// newlines in prose-like patterns, or looks like JSON/YAML, it's
	// probably not a base64 blob.
	if looksLikeText(s) {
		return "", false
	}

	if !looksBase64.MatchString(strings.TrimSpace(s)) {
		return "", false
	}

	// Confirm it actually decodes as base64.
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		// Try URL-safe variant
		decoded, err = base64.URLEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return "", false
		}
	}

	return fmt.Sprintf("[BASE64_DATA %d bytes decoded]", len(decoded)), true
}

// looksLikeText returns true if the string appears to be human-readable text
// rather than an encoded blob.  We check for a minimum density of common text
// characters (spaces, punctuation).
func looksLikeText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	// Sample up to 512 bytes for speed.
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
	// If more than 5% of characters are common text punctuation/whitespace,
	// it's very likely prose or structured text, not base64.
	return float64(textChars)/float64(len(sample)) > 0.05
}

// SanitizeText scans a raw text output (e.g. from kubectl) for inline base64
// blobs and replaces them with placeholders.  It also enforces a maximum
// output size.
func SanitizeText(s string) string {
	s = redactSensitiveText(s)
	s = redactSecretYAMLBlocks(s)
	s = replaceInlineBase64(s)
	if len(s) > maxResponseBytes {
		total := len(s)
		s = s[:maxResponseBytes] + fmt.Sprintf("\n[OUTPUT TRUNCATED - %d bytes total, showing first %d]", total, maxResponseBytes)
	}
	return s
}

func redactSensitiveText(s string) string {
	s = sensitiveJSONRe.ReplaceAllString(s, `${1}[REDACTED]${3}`)
	s = authorizationLineRe.ReplaceAllString(s, `${1}[REDACTED]`)
	return sensitiveAssignmentRe.ReplaceAllString(s, `${1}${2}[REDACTED]`)
}

func redactSecretYAMLBlocks(s string) string {
	lines := strings.SplitAfter(s, "\n")
	inSecretDoc := false
	inSecretData := false
	dataIndent := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if trimmed == "---" {
			inSecretDoc = false
			inSecretData = false
			continue
		}
		if trimmed == "kind: Secret" {
			inSecretDoc = true
			continue
		}
		if !inSecretDoc {
			continue
		}

		indent := leadingSpaces(line)
		if inSecretData && trimmed != "" && indent <= dataIndent {
			inSecretData = false
		}
		if trimmed == "data:" || trimmed == "stringData:" {
			inSecretData = true
			dataIndent = indent
			continue
		}
		if inSecretData {
			lines[i] = redactYAMLValueLine(line)
		}
	}

	return strings.Join(lines, "")
}

func leadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

func redactYAMLValueLine(line string) string {
	newline := ""
	if strings.HasSuffix(line, "\n") {
		newline = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return line + newline
	}
	prefix := line[:idx+1]
	value := strings.TrimSpace(line[idx+1:])
	if value == "" {
		return line + newline
	}
	return prefix + fmt.Sprintf(" [REDACTED %d bytes]", len(value)) + newline
}

// replaceInlineBase64 finds long runs of base64 characters in text output
// (common when kubectl outputs Secret YAML) and replaces them.
var inlineBase64Re = regexp.MustCompile(`(?m)^(\s*\S+:\s*)([A-Za-z0-9+/\-_]{256,}={0,2})\s*$`)

func replaceInlineBase64(s string) string {
	return inlineBase64Re.ReplaceAllStringFunc(s, func(match string) string {
		parts := inlineBase64Re.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		prefix := parts[1]
		blob := parts[2]
		return fmt.Sprintf("%s[BASE64_DATA %d chars]", prefix, len(blob))
	})
}

// TruncateJSON truncates a JSON byte slice if it exceeds maxResponseBytes.
func TruncateJSON(data []byte) []byte {
	if len(data) <= maxResponseBytes {
		return data
	}
	msg := fmt.Sprintf(`{"error":"response too large","totalBytes":%d,"maxBytes":%d,"hint":"Use more specific queries or filters to reduce output size"}`, len(data), maxResponseBytes)
	return []byte(msg)
}
