package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/guilhermesalviano/korchestrate/internal/contracts"
)

// Claude adapts the `claude` CLI. It is used for planning and runs read-only.
type Claude struct{}

func (Claude) Name() string { return "claude" }
func (Claude) Kind() Kind   { return Planner }

func (c Claude) Run(ctx context.Context, r Request) (*Result, error) {
	args := []string{
		"--print",
		"--model", r.Model,
		"--output-format", "json",
	}
	if r.SchemaInline != "" {
		args = append(args, "--json-schema", r.SchemaInline)
	}
	if r.System != "" {
		args = append(args, "--append-system-prompt", r.System)
	}
	// Planning is strictly read-only.
	args = append(args,
		"--permission-mode", "plan",
		"--allowed-tools", "Read Grep Glob Bash(git status:*) Bash(git diff:*) Bash(git log:*)",
	)
	if r.BudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", r.BudgetUSD))
	}
	args = append(args, r.ExtraArgs...)
	args = append(args, r.Prompt)

	r.Observe.Status(Planner, "claude plan started ("+r.Model+")")
	proc := Exec(ctx, ProcSpec{
		Bin:     "claude",
		Args:    args,
		Dir:     r.Dir,
		Env:     r.Env,
		Timeout: r.Timeout,
		OnLine:  lineObserver(r.Observe, Planner),
	})

	res := &Result{
		ExitCode: proc.ExitCode,
		Stderr:   proc.Stderr,
		Duration: proc.Duration,
	}
	if strings.TrimSpace(proc.Stdout) != "" {
		res.Events = append(res.Events, json.RawMessage(strings.TrimSpace(proc.Stdout)))
	}
	if proc.Err != nil && proc.ExitCode != 0 {
		return res, fmt.Errorf("claude exited %d: %s", proc.ExitCode, firstLine(proc.Stderr))
	}

	var out struct {
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		TotalCostUSD     float64         `json:"total_cost_usd"`
		Usage            struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(proc.Stdout)), &out); err != nil {
		// Not the expected envelope; try to salvage a JSON object from raw text.
		raw, exErr := contracts.ExtractJSON(proc.Stdout)
		if exErr != nil {
			return res, fmt.Errorf("claude: cannot parse output: %w", err)
		}
		res.Final = proc.Stdout
		res.Structured = raw
		return res, nil
	}

	res.Final = out.Result
	res.Usage = Usage{
		InputTokens:  out.Usage.InputTokens + out.Usage.CacheReadInputTokens + out.Usage.CacheCreationInputTokens,
		OutputTokens: out.Usage.OutputTokens,
		CostUSD:      out.TotalCostUSD,
	}
	if len(out.StructuredOutput) > 0 && string(out.StructuredOutput) != "null" {
		res.Structured = out.StructuredOutput
		return res, nil
	}
	if raw, err := contracts.ExtractJSON(out.Result); err == nil {
		res.Structured = raw
	}
	return res, nil
}

// lineObserver turns raw CLI lines into observer events, trimming noise.
func lineObserver(obs Observer, kind Kind) func(stream, line string) {
	if obs == nil {
		return nil
	}
	return func(stream, line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		obs(Event{Kind: kind, Stream: stream, Line: line})
	}
}

func firstLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
