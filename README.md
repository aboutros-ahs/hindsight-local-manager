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

## Releases

GitHub Releases publish:

- a small installer that downloads split runtime components and installs them under the user's profile
- split runtime component archives used by the installer for per-component download progress
- split runtime component archives for manual/offline installs
- the raw app exe for updater fallback

The first installer run can take several minutes because it downloads Python, Node, Hindsight, and UI packages. The installer logs progress for each component. Future in-app updates prefer the app-only archive so runtime dependencies are not redownloaded unless the installer/runtime assets are used again.

## Packaging Direction

`packaging\prepare-bundle.ps1` stages bundled Python, Node, Hindsight, and Control Plane resources for a smoother no-admin v1.
