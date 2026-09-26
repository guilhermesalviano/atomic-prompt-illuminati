# korchestrate

**kor** is a terminal orchestrator for AI coding agents. It takes a prompt and runs a
plan → execute → review pipeline, each stage powered by the agent of your choice,
inside an isolated git worktree.

- **Plan** with [Claude](https://claude.com)
- **Execute** with [Codex](https://openai.com/codex)
- **Review** with [OpenCode](https://opencode.ai)

Your main checkout stays untouched — kor works in a worktree and only applies changes
(or opens a PR) when the review passes.

## Install

Requires Go 1.22+ and the agent CLIs you want to use (`claude`, `codex`, `opencode`).

```sh
./install.sh
```

## Quick start

```sh
cp examples/kor.yaml kor.yaml   # edit models/agents per stage
kor run "add pagination to the users list"
```

kor plans the change, executes it in an isolated worktree, and has a reviewer agent
check the diff. Approve in the TUI to apply the result to your branch.

## Commands

| Command | Description |
| --- | --- |
| `kor run [prompt]` | plan, execute and review a prompt end to end |
| `kor` / `kor dashboard` | open the TUI dashboard |
| `kor resume` | resume a previous run |
| `kor list` | list runs |
| `kor status` | show status of a run |
| `kor clean` | remove worktrees and artifacts |
| `kor doctor` | check agents, config and repo health |

## Configuration

kor reads `./kor.yaml` or `~/.config/kor/config.yaml`. See [examples/kor.yaml](examples/kor.yaml)
for all options — per-stage agents and models, iteration limits, review gates, timeouts
and budgets.

## Uninstall

```sh
./uninstall.sh
```
