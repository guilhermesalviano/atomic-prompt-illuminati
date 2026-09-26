package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/guilhermesalviano/korchestrate/internal/agent"
)

// logLevel controls how an activity line is styled.
type logLevel int

const (
	levelInfo logLevel = iota
	levelStage
	levelDetail
	levelError
)

// logLine is one entry in a run's activity feed.
type logLine struct {
	at    time.Time
	kind  agent.Kind // empty for orchestrator messages
	level logLevel
	text  string
}

// summarizeEvent turns a raw agent output line into a short activity line.
// Agents stream JSON events on stdout; most are noise, so only the ones a
// human cares about (commands, messages, tool calls, file edits) are kept.
func summarizeEvent(ev agent.Event) (logLine, bool) {
	line := sanitize(ev.Line)
	if line == "" {
		return logLine{}, false
	}
	switch ev.Stream {
	case "status":
		return logLine{kind: ev.Kind, level: levelInfo, text: line}, true
	case "stderr":
		return logLine{kind: ev.Kind, level: levelError, text: line}, true
	}
	if !strings.HasPrefix(line, "{") {
		return logLine{kind: ev.Kind, level: levelDetail, text: line}, true
	}
	var obj map[string]any
	if json.Unmarshal([]byte(ev.Line), &obj) != nil {
		return logLine{}, false
	}
	text, lvl := describeJSON(obj)
	if text == "" {
		return logLine{}, false
	}
	return logLine{kind: ev.Kind, level: lvl, text: sanitize(text)}, true
}

// describeJSON recognises the event shapes emitted by codex, opencode,
// claude and antigravity. Unknown shapes are dropped.
func describeJSON(obj map[string]any) (string, logLevel) {
	typ := str(obj, "type")

	// codex exec --json: {"type":"item.started|item.completed","item":{...}}
	if item, ok := obj["item"].(map[string]any); ok {
		it := str(item, "type")
		switch {
		case it == "command_execution" && typ == "item.started":
			return "$ " + firstLineOf(str(item, "command")), levelDetail
		case it == "command_execution" && typ == "item.completed":
			if code, ok := item["exit_code"].(float64); ok && code != 0 {
				return fmt.Sprintf("✘ exit %d: %s", int(code), firstLineOf(str(item, "command"))), levelError
			}
		case it == "agent_message" && typ == "item.completed":
			return "» " + firstLineOf(str(item, "text")), levelInfo
		case it == "reasoning" && typ == "item.completed":
			return "… " + firstLineOf(str(item, "text")), levelDetail
		case it == "file_change" && typ == "item.completed":
			return "✎ " + changedPaths(item["changes"]), levelInfo
		case it == "mcp_tool_call" && typ == "item.started":
			return "⚙ " + str(item, "server") + "." + str(item, "tool"), levelDetail
		case it == "web_search" && typ == "item.started":
			return "⌕ " + str(item, "query"), levelDetail
		case it == "error":
			return str(item, "message"), levelError
		}
		return "", levelDetail
	}
	switch typ {
	case "turn.completed":
		if u, ok := obj["usage"].(map[string]any); ok {
			return fmt.Sprintf("turn done · %s in / %s out", tokens(num(u, "input_tokens")), tokens(num(u, "output_tokens"))), levelDetail
		}
	case "turn.failed", "error":
		if e, ok := obj["error"].(map[string]any); ok {
			return str(e, "message"), levelError
		}
		return str(obj, "message"), levelError
	}

	// opencode run --format json: {"type":"tool_use|text", "part":{...}}
	if part, ok := obj["part"].(map[string]any); ok {
		switch typ {
		case "tool_use":
			title := ""
			if st, ok := part["state"].(map[string]any); ok {
				title = str(st, "title")
			}
			return strings.TrimSpace("⚙ " + str(part, "tool") + " " + firstLineOf(title)), levelDetail
		case "text":
			return "» " + firstLineOf(str(part, "text")), levelInfo
		}
		return "", levelDetail
	}

	// claude --output-format json: one result envelope at the end.
	if typ == "result" {
		msg := "planner finished"
		if sub := str(obj, "subtype"); sub != "" {
			msg += " (" + sub + ")"
		}
		if c, ok := obj["total_cost_usd"].(float64); ok && c > 0 {
			msg += fmt.Sprintf(" · $%.2f", c)
		}
		return msg, levelInfo
	}

	// agy --output-format json: one envelope with status and usage.
	if _, ok := obj["conversation_id"]; ok {
		if st := str(obj, "status"); st != "" {
			msg := "antigravity finished (" + strings.ToLower(st) + ")"
			if u, ok := obj["usage"].(map[string]any); ok {
				msg += fmt.Sprintf(" · %s in / %s out", tokens(num(u, "input_tokens")), tokens(num(u, "output_tokens")))
			}
			if st != "SUCCESS" {
				return msg, levelError
			}
			return msg, levelInfo
		}
	}
	return "", levelDetail
}

// replayEvents rebuilds an activity feed for a historical run from the event
// logs the pipeline persisted. Timestamps are not recorded, so they are zero.
func replayEvents(dir string) []logLine {
	files, _ := filepath.Glob(filepath.Join(dir, "*.events*.jsonl"))
	order := map[string]int{"planner": 0, "executor": 1, "reviewer": 2}
	sort.SliceStable(files, func(i, j int) bool {
		ki, ii := eventFileKey(files[i])
		kj, ij := eventFileKey(files[j])
		if ii != ij {
			return ii < ij
		}
		return order[ki] < order[kj]
	})
	var out []logLine
	for _, f := range files {
		kind, iter := eventFileKey(f)
		k := agent.Kind(kind)
		out = append(out, logLine{kind: k, level: levelStage, text: fmt.Sprintf("%s · iteration %d", kind, iter)})
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, raw := range strings.Split(string(data), "\n") {
			if l, ok := summarizeEvent(agent.Event{Kind: k, Stream: "stdout", Line: raw}); ok {
				out = append(out, l)
			}
		}
	}
	return out
}

// eventFileKey parses "executor.events.2.jsonl" into ("executor", 2). The
// planner file has no iteration and sorts first.
func eventFileKey(path string) (string, int) {
	parts := strings.Split(filepath.Base(path), ".")
	iter := -1
	if len(parts) >= 4 {
		fmt.Sscanf(parts[2], "%d", &iter)
	}
	return parts[0], iter
}

func changedPaths(v any) string {
	list, _ := v.([]any)
	var paths []string
	for _, c := range list {
		if m, ok := c.(map[string]any); ok {
			p := str(m, "path")
			if k := str(m, "kind"); k != "" {
				p = k + " " + p
			}
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return "files changed"
	}
	return strings.Join(paths, ", ")
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func num(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

// sanitize strips terminal escapes and control characters from agent output so
// it cannot break the layout.
func sanitize(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}
