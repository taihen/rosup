package validate

import (
	"regexp"
	"strings"
	"unicode"
)

var cidrRE = regexp.MustCompile(`\d+\.\d+\.\d+\.\d+/\d+`)
var macRE = regexp.MustCompile(`(?i)(?:[0-9a-f]{2}:){5}[0-9a-f]{2}`)

type printRecord struct {
	flags  string
	fields map[string]string
}

func parseKeyedRecords(output string) []printRecord {
	var recs []printRecord
	var cur *printRecord
	for _, line := range splitPrintLines(output) {
		if rec, ok := startKeyedRecord(line); ok {
			recs = append(recs, rec)
			cur = &recs[len(recs)-1]
			continue
		}
		if cur != nil {
			mergeFields(cur.fields, parseAssignments(line))
		}
	}
	return recs
}

func startKeyedRecord(line string) (printRecord, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !isIndex(fields[0]) {
		return printRecord{}, false
	}
	rest := strings.TrimSpace(line)
	rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	rec := printRecord{fields: map[string]string{}}
	for rest != "" {
		tok, after, _ := strings.Cut(rest, " ")
		if strings.Contains(tok, "=") {
			break
		}
		if isUpperFlag(tok) {
			rec.flags += tok
			rest = strings.TrimSpace(after)
			continue
		}
		break
	}
	mergeFields(rec.fields, parseAssignments(rest))
	if len(rec.fields) == 0 {
		return printRecord{}, false
	}
	return rec, true
}

func parseAssignments(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(s[i : i+eq])
		i = i + eq + 1
		if i < len(s) && s[i] == '"' {
			i++
			j := i
			for j < len(s) && s[j] != '"' {
				j++
			}
			out[key] = s[i:j]
			if j < len(s) {
				i = j + 1
			} else {
				i = j
			}
			continue
		}
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		out[key] = s[i:j]
		i = j
	}
	return out
}

func parseKVBlock(output string) map[string]string {
	out := map[string]string{}
	for _, line := range splitPrintLines(output) {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func parseRoutePrefixes(output string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, line := range splitPrintLines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 || !isIndex(fields[0]) {
			continue
		}
		flags := ""
		if isLetterToken(fields[1]) {
			flags = fields[1]
		}
		if strings.Contains(strings.ToUpper(flags), "X") {
			continue
		}
		prefix := cidrRE.FindString(line)
		if prefix == "" {
			continue
		}
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	return out
}

func parseNamedRows(output string) []printRecord {
	var recs []printRecord
	for _, line := range splitPrintLines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 || !isIndex(fields[0]) {
			continue
		}
		flags := ""
		nameIdx := 1
		if isUpperFlag(fields[1]) {
			flags = fields[1]
			nameIdx = 2
		}
		if nameIdx >= len(fields) {
			continue
		}
		name := fields[nameIdx]
		rec := printRecord{flags: flags, fields: map[string]string{"name": name}}
		if prefix := cidrRE.FindString(line); prefix != "" {
			rec.fields["address"] = prefix
		}
		recs = append(recs, rec)
	}
	return recs
}

func parseMACs(output string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, line := range splitPrintLines(output) {
		fields := strings.Fields(line)
		if len(fields) == 0 || !isIndex(fields[0]) {
			continue
		}
		mac := strings.ToUpper(macRE.FindString(line))
		if mac == "" {
			continue
		}
		if _, ok := seen[mac]; ok {
			continue
		}
		seen[mac] = struct{}{}
		out = append(out, mac)
	}
	return out
}

func disabled(flags string, fields map[string]string) bool {
	if strings.Contains(flags, "X") {
		return true
	}
	if fields != nil {
		switch strings.ToLower(fields["disabled"]) {
		case "yes", "true":
			return true
		}
	}
	return false
}

func running(flags string) bool {
	return strings.Contains(flags, "R")
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isUpperFlag(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func isLetterToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func splitPrintLines(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "Flags:") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func mergeFields(dst, src map[string]string) {
	if dst == nil {
		return
	}
	for k, v := range src {
		dst[k] = v
	}
}

func splitSet(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := map[string]struct{}{}
	for _, v := range a {
		left[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := left[v]; !ok {
			return false
		}
	}
	return true
}
