package validate

import (
	"fmt"
	"strings"
)

type logLine struct {
	Time    string
	Topics  string
	Message string
}

func InstallErrors(output string) error {
	var install []logLine
	for _, raw := range strings.Split(output, "\n") {
		line, ok := parseSystemLogLine(raw)
		if !ok {
			continue
		}
		if !strings.Contains(strings.ToLower(line.Message), "install") {
			continue
		}
		install = append(install, line)
	}
	if len(install) == 0 {
		return nil
	}
	lastTime := install[len(install)-1].Time
	for _, line := range install {
		if line.Time != lastTime {
			continue
		}
		if hasErrorTopic(line.Topics) {
			return fmt.Errorf("install error: %s", line.Message)
		}
	}
	return nil
}

func parseSystemLogLine(raw string) (logLine, bool) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if raw == "" {
		return logLine{}, false
	}
	idx := strings.Index(raw, "system,")
	if idx < 0 {
		return logLine{}, false
	}
	when := strings.TrimSpace(raw[:idx])
	rest := raw[idx:]
	topics, message, ok := strings.Cut(rest, " ")
	if !ok {
		return logLine{}, false
	}
	return logLine{
		Time:    when,
		Topics:  topics,
		Message: strings.TrimSpace(message),
	}, true
}

func hasErrorTopic(topics string) bool {
	for _, t := range strings.Split(topics, ",") {
		if t == "error" {
			return true
		}
	}
	return false
}
