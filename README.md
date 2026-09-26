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
# Edit config.json to set the default agents and models per stage.
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

Edit [config.json](config.json) to define the default provider CLI (`agent`) and
`model` for each stage: `planner`, `executor`, and `reviewer`. Supported agents are
`claude`, `codex`, and `opencode`. Each stage can also set a `fallback` agent;
`variant` controls reasoning effort and `subagent` selects an OpenCode agent.
These values are preselected in the dashboard's model picker.

Without `--config`, kor uses the first file found in this order:

1. `<repo>/kor.yaml`
2. `<repo>/config.json`
3. `~/.config/kor/config.yaml`
4. `~/.config/kor/config.json`

`XDG_CONFIG_HOME` replaces `~/.config` when set. To share defaults across repos,
copy `config.json` to the user config directory. To select a file explicitly,
use `kor --config /path/to/config.json run "your prompt"`.

JSON and YAML use the same fields, and omitted fields keep their built-in defaults.
See [examples/kor.yaml](examples/kor.yaml) for all options, including iteration
limits, review gates, timeouts and budgets.

## Uninstall

```sh
./uninstall.sh
```
