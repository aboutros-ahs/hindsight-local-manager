# Packaging

Run from PowerShell:

```powershell
.\packaging\prepare-bundle.ps1
wails build
```

The script stages user-space runtime resources under `resources/`:

- portable Node 22+
- Python embeddable runtime
- `hindsight-api`
- Hindsight Control Plane npm package
- prewarmed local embedding/reranker model cache

The app also has development fallbacks to `hindsight-local-mcp` and `npx` when bundled resources are absent.
