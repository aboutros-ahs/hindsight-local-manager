# Wails UI Plan

The desktop UI is implemented with Wails.

## Install Wails

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Then make sure `%USERPROFILE%\go\bin` is on `PATH`, or run Wails directly from that path.

## UI

- Start/stop native bridge server
- Show current status: bridge URL, OpenCode server status, selected OpenCode server working directory
- OpenCode server working directory picker for `OPENCODE_PROJECT_DIR`
- Default model dropdown loaded from `/v1/models`
- Model alias editor
- Copy OpenAI-compatible base URL button
- Logs panel
- Minimize to system tray
- Start with Windows toggle

## Tray Support

Tray support is implemented with `github.com/getlantern/systray`. The tray menu includes:

- Open
- Start bridge
- Stop bridge
- Copy base URL
- Copy API key
- Quit

On Windows/Wails v2, the titlebar X hides to tray. Use the in-app `QUIT` button or tray `Quit` to exit completely. Minimize-to-tray is supported by watching for native minimization and hiding the window when `APP_MINIMIZE_TO_TRAY=true`.

## Start With Windows

For per-user autostart, write a shortcut to:

```text
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
```

The shortcut target can launch the packaged Wails executable with a `--minimized` flag.

Alternative: write a `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` registry value. The Startup shortcut is easier to inspect and remove.

## Packaging Direction

The OpenAI-compatible bridge server is native Go now. Node is only used by Vite during frontend development/builds.
