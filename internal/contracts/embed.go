package contracts

import _ "embed"

// Embedded JSON schemas and system prompts. Keeping them beside the code means
// a single `api` binary carries its own contracts.

//go:embed schemas/plan.schema.json
var PlanSchema string

//go:embed schemas/review.schema.json
var ReviewSchema string

//go:embed schemas/codex-report.schema.json
var ExecReportSchema string

//go:embed prompts/planner.md
var PlannerPrompt string

//go:embed prompts/executor.md
var ExecutorPrompt string

//go:embed prompts/reviewer.md
var ReviewerPrompt string
