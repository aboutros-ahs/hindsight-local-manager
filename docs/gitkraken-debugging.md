# GitKraken Debugging

GitKraken successfully generating a commit subject means the OpenAI-compatible endpoint is working.

If the description/body is missing, enable bridge logs and trigger the GitKraken action again:

```env
BRIDGE_LOG_DIR=.bridge-logs
BRIDGE_LOG_BODIES=true
```

Then restart the bridge from the desktop app or relaunch:

```powershell
.\build\bin\opencode-api-bridge.exe
```

Each request writes two files:

- `*-request.json`: selected model, message count, prompt/system text
- `*-response.json`: OpenCode session/message IDs and returned content

What to check:

- If the prompt asks for both subject and body but `rawContent` is one line, this is a model/prompt behavior issue.
- If the prompt only asks for one line, GitKraken did not request a description for that action.
- If `rawContent` contains a body but GitKraken does not display it, GitKraken may expect a specific commit-message format.

The bridge currently adds a blank commit-body separator for one-line commit-message responses:

```text
subject

```

That preserves the generated subject while giving GitKraken a conventional subject/body split point.
