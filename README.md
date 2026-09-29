# TT

TT is a local-first activity tracker. The VS Code extension and shell hooks send small heartbeat events to `tt beat`; the CLI writes minute buckets and an append-only Linh Khí ledger to SQLite. There is no daemon, server, account, filename, project name, or keystroke collection.

## Requirements

- Go 1.23 or newer
- Bash, zsh, or fish for the optional shell hook
- Node.js and npm to build the VS Code extension

## Install the CLI

```sh
go install ./cmd/tt
tt init
```

The database is stored in the OS user config directory under `tt/tt.db`. Set `TT_DB` to override it. The init wizard sets a preferred daily quest cutoff, a dao title, and can append a throttled hook to the current shell's rc file. Open a new terminal after installation. The hook calls `tt beat` in the background at most once every 30 seconds.

## VS Code extension

```sh
cd extensions/vscode
npm install
npm run compile
```

Package or open this directory as a VS Code extension. The extension listens for text edits, saves, editor changes, and focus changes; it emits at most one heartbeat every 60 seconds while the window is focused. It sends only the source and editor language identifier.

For VS Code Remote/SSH, the extension runs on the remote workspace host. Install the extension there and make sure the `tt` executable is installed and available on that remote machine's `PATH`.

## Commands

- `tt`: interactive dashboard with progression, quest, titles, and weekly stats tabs
- `tt init`: setup wizard
- `tt status`: compact progression status
- `tt focus [--min 25]`: timed focus session
- `tt breakthrough`: attempt the current realm breakthrough
- `tt stats [--week]`: activity and Linh Khí summaries
- `tt beat`: silent, failure-tolerant heartbeat command for integrations
- `tt doctor`: database, shell hook, and recent VS Code heartbeat checks
- `tt export`: export all local tables as JSON to stdout

Rules and starting progression values are in `content/rules.yaml`. `internal/core` contains deterministic game calculations and accepts time from its caller; storage and integrations are outside that package.

## MVP boundaries

This version does not include a server, GitHub login, leaderboard, guild, badge SVGs, Discord, random encounters, social-site blocking, JetBrains support, Windows shell hooks, or anti-cheat. Heartbeats are an activity signal, not proof of focused work. Breakthrough failure currently records a failed completed session and does not deduct Linh Khí.