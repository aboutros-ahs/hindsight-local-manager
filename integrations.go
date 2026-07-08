package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func installOpenCodePlugin(path, apiURL string, includeMCP bool) error {
	cfg := map[string]any{}
	if err := readOpenCodeConfigFile(path, &cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg["$schema"] = valueOr(asString(cfg["$schema"]), "https://opencode.ai/config.json")
	cfg["plugin"] = upsertOpenCodeHindsightPlugin(cfg["plugin"], apiURL)
	if includeMCP {
		cfg["mcp"] = upsertOpenCodeMCP(cfg["mcp"], apiURL)
	}
	if err := backupIfExists(path); err != nil {
		return err
	}
	return writeJSONFile(path, cfg, 0600)
}

func inspectOpenCodePlugin() IntegrationStatus {
	path := openCodeConfigPath()
	cfg := map[string]any{}
	if err := readOpenCodeConfigFile(path, &cfg); err != nil {
		return IntegrationStatus{Installed: false, Path: path, Detail: "not configured"}
	}
	if pluginContainsHindsight(cfg["plugin"]) {
		return IntegrationStatus{Installed: true, Path: path, Detail: "@vectorize-io/opencode-hindsight configured"}
	}
	return IntegrationStatus{Installed: false, Path: path, Detail: "plugin missing"}
}

func inspectOpenCodeMCP() IntegrationStatus {
	path := openCodeConfigPath()
	cfg := map[string]any{}
	if err := readOpenCodeConfigFile(path, &cfg); err != nil {
		return IntegrationStatus{Installed: false, Path: path, Detail: "not configured"}
	}
	if openCodeMCPContainsHindsight(cfg["mcp"]) {
		return IntegrationStatus{Installed: true, Path: path, Detail: "hindsight MCP entry present"}
	}
	return IntegrationStatus{Installed: false, Path: path, Detail: "MCP entry missing"}
}

func installCodexHooks(installRoot, apiURL string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	source := bundledCodexHooksDir(installRoot)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("bundled Codex hooks not found at %s", source)
	}
	installDir := filepath.Join(home, ".hindsight", "codex")
	scriptsDir := filepath.Join(installDir, "scripts")
	if err := copyDir(filepath.Join(source, "scripts"), scriptsDir); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(source, "settings.json"), filepath.Join(installDir, "settings.json")); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(home, ".hindsight", "codex.json"), map[string]any{
		"hindsightApiUrl": apiURL,
		"bankId":          "codex",
		"autoRecall":      true,
		"autoRetain":      true,
	}, 0600); err != nil {
		return err
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		return err
	}
	python := "python"
	if _, err := os.Stat(filepath.Join(installRoot, "resources", "python", "python.exe")); err == nil {
		python = filepath.Join(installRoot, "resources", "python", "python.exe")
	}
	hooks := map[string]any{"hooks": map[string]any{
		"SessionStart":     []any{hookCommand(python, filepath.Join(scriptsDir, "session_start.py"), 5)},
		"UserPromptSubmit": []any{hookCommand(python, filepath.Join(scriptsDir, "recall.py"), 12)},
		"Stop":             []any{hookCommand(python, filepath.Join(scriptsDir, "retain.py"), 30)},
	}}
	if err := backupIfExists(filepath.Join(codexDir, "hooks.json")); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(codexDir, "hooks.json"), hooks, 0600); err != nil {
		return err
	}
	return ensureCodexHooksFeature(filepath.Join(codexDir, "config.toml"))
}

func bundledCodexHooksDir(installRoot string) string {
	candidates := []string{
		filepath.Join(installRoot, "resources", "integrations", "codex"),
		filepath.Join(installRoot, "..", "resources", "integrations", "codex"),
		filepath.Join(installRoot, "..", "..", "resources", "integrations", "codex"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			if path, err := filepath.Abs(candidate); err == nil {
				return path
			}
			return candidate
		}
	}
	return candidates[0]
}

func inspectCodexHooks() IntegrationStatus {
	home, err := os.UserHomeDir()
	if err != nil {
		return IntegrationStatus{Detail: err.Error()}
	}
	path := filepath.Join(home, ".codex", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return IntegrationStatus{Installed: false, Path: path, Detail: "hooks missing"}
	}
	if strings.Contains(string(data), "hindsight") || strings.Contains(string(data), ".hindsight") {
		return IntegrationStatus{Installed: true, Path: path, Detail: "Codex hooks configured"}
	}
	return IntegrationStatus{Installed: false, Path: path, Detail: "hooks file exists without Hindsight"}
}

func openCodeConfigPath() string {
	jsonPath, jsoncPath := openCodeConfigPaths()
	if fileExists(jsoncPath) {
		return jsoncPath
	}
	if fileExists(jsonPath) {
		return jsonPath
	}
	return jsoncPath
}

func openCodeConfigPaths() (string, string) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "opencode")
	return filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc")
}

func validateOpenCodeConfigPath(path string) error {
	jsonPath, jsoncPath := openCodeConfigPaths()
	path = filepath.Clean(path)
	if strings.EqualFold(path, filepath.Clean(jsonPath)) || strings.EqualFold(path, filepath.Clean(jsoncPath)) {
		return nil
	}
	return fmt.Errorf("unsupported OpenCode config path: %s", path)
}

func upsertOpenCodeHindsightPlugin(value any, apiURL string) []any {
	entry := []any{"@vectorize-io/opencode-hindsight", map[string]any{
		"hindsightApiUrl": apiURL,
		"bankId":          "opencode",
		"autoRecall":      true,
		"autoRetain":      true,
		"recallBudget":    "mid",
	}}
	items := toArray(value)
	result := make([]any, 0, len(items)+1)
	replaced := false
	for _, item := range items {
		if isHindsightPlugin(item) {
			result = append(result, entry)
			replaced = true
			continue
		}
		result = append(result, item)
	}
	if !replaced {
		result = append(result, entry)
	}
	return result
}

func upsertOpenCodeMCP(value any, apiURL string) map[string]any {
	mcp := map[string]any{}
	if existing, ok := value.(map[string]any); ok {
		for key, val := range existing {
			mcp[key] = val
		}
	}
	mcp["hindsight"] = map[string]any{
		"type":    "remote",
		"url":     strings.TrimRight(apiURL, "/") + "/mcp/",
		"enabled": true,
	}
	return mcp
}

func openCodeMCPContainsHindsight(value any) bool {
	mcp, ok := value.(map[string]any)
	if !ok {
		return false
	}
	entry, ok := mcp["hindsight"].(map[string]any)
	if !ok {
		return false
	}
	return asString(entry["type"]) == "remote" && strings.Contains(asString(entry["url"]), "/mcp") && asBool(entry["enabled"])
}

func readOpenCodeConfigFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ".jsonc") {
		data = []byte(stripJSONCTrailingCommas(stripJSONComments(string(data))))
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func stripJSONComments(input string) string {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '/' {
			for i < len(input) && input[i] != '\n' {
				i++
			}
			if i < len(input) {
				out.WriteByte(input[i])
			}
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '*' {
			i += 2
			for i+1 < len(input) && !(input[i] == '*' && input[i+1] == '/') {
				if input[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			i++
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func stripJSONCTrailingCommas(input string) string {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(input) && (input[j] == ' ' || input[j] == '\t' || input[j] == '\r' || input[j] == '\n') {
				j++
			}
			if j < len(input) && (input[j] == '}' || input[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func pluginContainsHindsight(value any) bool {
	for _, item := range toArray(value) {
		if isHindsightPlugin(item) {
			return true
		}
	}
	return false
}

func isHindsightPlugin(value any) bool {
	if asString(value) == "@vectorize-io/opencode-hindsight" {
		return true
	}
	if values, ok := value.([]any); ok && len(values) > 0 {
		return asString(values[0]) == "@vectorize-io/opencode-hindsight"
	}
	return false
}

func toArray(value any) []any {
	if value == nil {
		return nil
	}
	if values, ok := value.([]any); ok {
		return values
	}
	return []any{value}
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func asBool(value any) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	return false
}

func hookCommand(python, script string, timeout int) map[string]any {
	return map[string]any{"hooks": []any{map[string]any{
		"type":    "command",
		"command": quoteArg(python) + " " + quoteArg(script),
		"timeout": timeout,
	}}}
}

func ensureCodexHooksFeature(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	if strings.Contains(text, "codex_hooks = true") {
		return nil
	}
	if err := backupIfExists(path); err != nil {
		return err
	}
	if !strings.Contains(text, "[features]") {
		text += "\n[features]\ncodex_hooks = true\n"
	} else {
		text = strings.Replace(text, "[features]", "[features]\ncodex_hooks = true", 1)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0600)
}

func quoteArg(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "\\\"") + "\""
}

func backupIfExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return copyFile(path, path+".bak")
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}
