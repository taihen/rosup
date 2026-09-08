package redact

import "regexp"

const redacted = "***"

var (
	kvSecret = regexp.MustCompile(`(?i)((?:authentication-password|wpa2?-pre-shared-key|passphrase|password|secret)\s*=\s*)(?:"[^"]*"|[^\s]+)`)
	bearer   = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)\S+`)
)

func String(in string) string {
	out := kvSecret.ReplaceAllString(in, "${1}"+redacted)
	return bearer.ReplaceAllString(out, "${1}"+redacted)
}
