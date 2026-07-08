# Windows Environment Setup

The bridge reads `.env` in the project root only as first-run defaults. After a user saves Settings in the app, `.bridge-config.json` becomes the source of truth so `.env` cannot override app changes. App profiles are stored separately in `.bridge-apps.json`.

Current local `.env`:

```text
OPENCODE_PROJECT_DIR=C:\Projects\opencode-api-bridge
OPENCODE_DEFAULT_MODEL=github-copilot/gpt-5.5
BRIDGE_SESSION_MODE=stateful
```

Start the desktop app with:

```powershell
.\build\bin\opencode-api-bridge.exe
```

## One-Off PowerShell Variables

For a single PowerShell session:

```powershell
$env:OPENCODE_PROJECT_DIR = 'C:\Projects\opencode-api-bridge'
$env:OPENCODE_DEFAULT_MODEL = 'github-copilot/gpt-5.5'
.\build\bin\opencode-api-bridge.exe
```

These values disappear when the terminal closes and are not read after `.bridge-config.json` exists.

## Persistent User Variables

To persist values for your Windows user:

```powershell
[Environment]::SetEnvironmentVariable('OPENCODE_PROJECT_DIR', 'C:\Projects\opencode-api-bridge', 'User')
[Environment]::SetEnvironmentVariable('OPENCODE_DEFAULT_MODEL', 'github-copilot/gpt-5.5', 'User')
```

Open a new terminal after setting persistent variables. These values are not read after `.bridge-config.json` exists.

## Client Custom URL

Use this first:

```text
http://127.0.0.1:7331/v1
```

If GitKraken expects the exact endpoint, use:

```text
http://127.0.0.1:7331/v1/chat/completions
```

If you set `BRIDGE_API_KEY`, use that same value as the API key in your client app.

If you do not set `BRIDGE_API_KEY`, the bridge generates one in `.bridge-api-key` and prints it at startup:

```text
API key: ocab_...
```

Use that value in your client app.
