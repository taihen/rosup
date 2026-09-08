package redact

import "regexp"

const redacted = "***"

var (
	kvSecret = regexp.MustCompile(`(?i)((?:tcp-md5-key|management-protection-key|static-sta-private-key|sta-private-key|authentication-password|authentication-key|static-key-0|static-key-1|static-key-2|static-key-3|wpa2?-pre-shared-key|passphrase|password|secret)\s*=\s*)(?:"[^"]*"|[^\s]+)`)
	bearer   = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)\S+`)
)

func String(in string) string {
	out := kvSecret.ReplaceAllString(in, "${1}"+redacted)
	return bearer.ReplaceAllString(out, "${1}"+redacted)
}
