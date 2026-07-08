# Hindsight Local Manager

Windows-only Wails app for running Hindsight locally without admin rights or Docker.

The app manages:

- a hidden Hindsight-only OpenCode API bridge
- `opencode serve`
- Hindsight API/MCP on `127.0.0.1:8888`
- optional Hindsight Control Plane UI on `127.0.0.1:9999`
- OpenCode plugin/MCP config
- Codex hooks

Runtime state is stored under `%LOCALAPPDATA%\HindsightLocalManager`.

## Development

```powershell
wails dev
```

## Build

```powershell
wails build
```

## Packaging Direction

`packaging\prepare-bundle.ps1` stages bundled Python, Node, Hindsight, and Control Plane resources for a smoother no-admin v1.
