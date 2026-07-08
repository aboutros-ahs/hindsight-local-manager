package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BridgeServer struct {
	root         string
	cfg          BridgeConfig
	apiKey       string
	appProfiles  []AppProfile
	baseURL      string
	opencodeURL  string
	log          func(string)
	server       *http.Server
	opencodeCmd  *exec.Cmd
	opencodeStop context.CancelFunc
	sessions     map[string]SessionEntry
	mu           sync.Mutex
	modelCache   []string
	modelExpiry  time.Time
}

type SessionEntry struct {
	Key          string       `json:"key"`
	Name         string       `json:"name,omitempty"`
	KeySource    string       `json:"keySource"`
	SessionID    string       `json:"sessionId"`
	Model        string       `json:"model"`
	Fingerprints []string     `json:"fingerprints"`
	MessageCount int          `json:"messageCount"`
	LastResponse *CachedReply `json:"lastResponse,omitempty"`
	Archived     bool         `json:"archived,omitempty"`
	UpdatedAt    string       `json:"updatedAt"`
}

type CachedReply struct {
	Content string       `json:"content"`
	Tokens  *UsageTokens `json:"tokens,omitempty"`
}

type sessionStoreFile struct {
	Sessions map[string]SessionEntry `json:"sessions"`
}

type ChatRequest struct {
	Model               string                 `json:"model"`
	Messages            []ChatMessage          `json:"messages"`
	Stream              bool                   `json:"stream"`
	User                string                 `json:"user"`
	Metadata            map[string]any         `json:"metadata"`
	Temperature         any                    `json:"temperature"`
	MaxCompletionTokens any                    `json:"max_completion_tokens"`
	MaxTokens           any                    `json:"max_tokens"`
	Extra               map[string]interface{} `json:"-"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type sessionPlan struct {
	Key            string
	KeySource      string
	Action         string
	Entry          *SessionEntry
	Messages       []ChatMessage
	CachedResponse *CachedReply
}

type requestContext struct {
	AppID   string
	AppName string
	Config  BridgeConfig
}

type OpenCodeSession struct {
	ID string `json:"id"`
}

type OpenCodeMessageResponse struct {
	Info  OpenCodeMessageInfo `json:"info"`
	Parts []OpenCodePart      `json:"parts"`
}

type OpenCodeMessageInfo struct {
	ID     string       `json:"id"`
	Finish string       `json:"finish"`
	Tokens *UsageTokens `json:"tokens"`
}

type UsageTokens struct {
	Input     int `json:"input,omitempty"`
	Output    int `json:"output,omitempty"`
	Reasoning int `json:"reasoning,omitempty"`
	Total     int `json:"total,omitempty"`
}

type OpenCodePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewBridgeServer(root string, cfg BridgeConfig, apiKey string, appProfiles []AppProfile, logger func(string)) (*BridgeServer, error) {
	port := valueOr(cfg.Port, defaultBridgePort)
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("invalid BRIDGE_PORT %q", port)
	}
	host := valueOr(cfg.Host, defaultBridgeHost)
	opencodeURL := strings.TrimRight(valueOr(cfg.OpenCodeServerURL, "http://127.0.0.1:4096"), "/")
	b := &BridgeServer{
		root:        root,
		cfg:         cfg,
		apiKey:      apiKey,
		appProfiles: appProfiles,
		baseURL:     fmt.Sprintf("http://%s:%s", host, port),
		opencodeURL: opencodeURL,
		log:         logger,
		sessions:    map[string]SessionEntry{},
	}
	b.loadSessions()
	return b, nil
}

func (b *BridgeServer) Start() error {
	addr := net.JoinHostPort(valueOr(b.cfg.Host, defaultBridgeHost), valueOr(b.cfg.Port, defaultBridgePort))
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.route)
	b.server = &http.Server{Addr: addr, Handler: mux}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	b.log("native bridge listening on http://" + addr)
	b.log("OpenAI-compatible base URL: " + b.baseURL + "/v1")
	b.log("session mode: " + valueOr(b.cfg.SessionMode, "stateful"))
	go func() {
		if err := b.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.log("bridge server error: " + err.Error())
		}
	}()
	return nil
}

func (b *BridgeServer) Stop(ctx context.Context) error {
	if b.opencodeStop != nil {
		b.opencodeStop()
	}
	if b.opencodeCmd != nil && b.opencodeCmd.Process != nil {
		_ = b.opencodeCmd.Process.Kill()
	}
	if b.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := b.server.Shutdown(ctx)
	b.log("native bridge stopped")
	return err
}

func (b *BridgeServer) Running() bool { return b.server != nil }

func (b *BridgeServer) Healthy() bool { return b.Running() }

func (b *BridgeServer) route(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		b.writeCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/health" || r.URL.Path == "/v1/health" {
		b.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "opencodeUrl": b.opencodeURL})
		return
	}
	reqCtx, ok := b.authorizeRequest(r)
	if !ok {
		b.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "Unauthorized", "type": "unauthorized"}})
		return
	}
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/v1/models" || r.URL.Path == "/models"):
		b.handleModels(w, reqCtx)
	case r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/chat/completions"):
		b.handleChat(w, r, reqCtx)
	case r.Method == http.MethodGet && r.URL.Path == "/bridge/sessions":
		b.handleListSessions(w)
	case r.Method == http.MethodDelete && r.URL.Path == "/bridge/sessions":
		b.handleDeleteSessions(w)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/bridge/sessions/"):
		b.handleDeleteSession(w, strings.TrimPrefix(r.URL.Path, "/bridge/sessions/"))
	default:
		b.writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "not found", "type": "not_found"}})
	}
}

func (b *BridgeServer) handleModels(w http.ResponseWriter, reqCtx requestContext) {
	models, err := b.listModels()
	if err != nil {
		b.writeError(w, err)
		return
	}
	seen := map[string]bool{}
	for _, model := range models {
		seen[model] = true
	}
	for alias := range modelAliases(reqCtx.Config) {
		if !seen[alias] {
			models = append(models, alias)
			seen[alias] = true
		}
	}
	sort.Strings(models)
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		owner := strings.SplitN(model, "/", 2)[0]
		data = append(data, map[string]any{"id": model, "object": "model", "created": 0, "owned_by": owner})
	}
	b.writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (b *BridgeServer) handleChat(w http.ResponseWriter, r *http.Request, reqCtx requestContext) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		b.writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid JSON request body", "type": "invalid_request_error"}})
		return
	}
	if req.Stream {
		b.writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "streaming responses are not implemented yet", "type": "invalid_request_error"}})
		return
	}
	if len(req.Messages) == 0 {
		b.writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "messages must be an array", "type": "invalid_request_error"}})
		return
	}
	reqCtx = b.contextWithRequestHints(reqCtx, r, req)
	cfg := reqCtx.Config
	modelID := req.Model
	if alias := modelAliases(cfg)[modelID]; alias != "" {
		modelID = alias
	}
	if modelID == "" {
		modelID = cfg.DefaultModel
	}
	providerID, openCodeModelID, ok := strings.Cut(modelID, "/")
	if !ok || providerID == "" || openCodeModelID == "" {
		b.writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "model must use provider/model format", "type": "invalid_request_error"}})
		return
	}
	plan := b.planSession(req, r, cfg, reqCtx.AppID)
	system, prompt := convertMessages(plan.Messages)
	requestID := shortHash(fmt.Sprintf("%d", time.Now().UnixNano()))
	b.writeDebug(requestID, "request", map[string]any{
		"app": reqCtx.AppID, "model": req.Model, "resolvedModel": modelID, "sessionMode": valueOr(cfg.SessionMode, "stateful"), "sessionKey": plan.Key,
		"sessionKeySource": plan.KeySource, "sessionAction": plan.Action, "messageCount": len(req.Messages), "forwardedMessageCount": len(plan.Messages),
		"system": maybeLogBody(system, cfg.LogBodies), "prompt": maybeLogBody(prompt, cfg.LogBodies),
	})
	if plan.CachedResponse != nil {
		b.writeJSON(w, http.StatusOK, buildChatResponse(requestID, valueOr(req.Model, modelID), plan.CachedResponse.Content, "stop", plan.CachedResponse.Tokens))
		return
	}
	if err := b.ensureOpenCode(); err != nil {
		b.writeError(w, err)
		return
	}
	sessionID := ""
	if plan.Entry != nil && plan.Action != "replace" {
		sessionID = plan.Entry.SessionID
	} else {
		session, err := b.createOpenCodeSession(prompt)
		if err != nil {
			b.writeError(w, err)
			return
		}
		sessionID = session.ID
	}
	resp, err := b.sendOpenCodeMessage(sessionID, providerID, openCodeModelID, system, prompt, cfg)
	if err != nil {
		b.writeError(w, err)
		return
	}
	content := normalizeResponseContent(extractOpenCodeText(resp.Parts), prompt)
	if plan.Key != "" {
		b.updateSession(plan.Key, plan.KeySource, sessionID, modelID, req.Messages, content, resp.Info.Tokens)
	}
	b.writeDebug(requestID, "response", map[string]any{"app": reqCtx.AppID, "sessionId": sessionID, "messageId": resp.Info.ID, "finish": resp.Info.Finish, "tokens": resp.Info.Tokens, "content": maybeLogBody(content, cfg.LogBodies)})
	b.writeJSON(w, http.StatusOK, buildChatResponse(resp.Info.ID, valueOr(req.Model, modelID), content, valueOr(resp.Info.Finish, "stop"), resp.Info.Tokens))
}

func (b *BridgeServer) handleListSessions(w http.ResponseWriter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := make([]map[string]any, 0, len(b.sessions))
	for _, entry := range b.sessions {
		data = append(data, map[string]any{"key": entry.Key, "keySource": entry.KeySource, "sessionId": entry.SessionID, "model": entry.Model, "messageCount": entry.MessageCount, "updatedAt": entry.UpdatedAt})
	}
	b.writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (b *BridgeServer) handleDeleteSessions(w http.ResponseWriter) {
	b.mu.Lock()
	b.sessions = map[string]SessionEntry{}
	b.mu.Unlock()
	b.saveSessions()
	b.writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (b *BridgeServer) handleDeleteSession(w http.ResponseWriter, key string) {
	b.mu.Lock()
	_, ok := b.sessions[key]
	delete(b.sessions, key)
	b.mu.Unlock()
	b.saveSessions()
	b.writeJSON(w, http.StatusOK, map[string]any{"deleted": ok})
}

func (b *BridgeServer) authorizeRequest(r *http.Request) (requestContext, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	for _, profile := range b.appProfiles {
		if !profile.Enabled || profile.APIKey == "" {
			continue
		}
		if len(token) == len(profile.APIKey) && subtle.ConstantTimeCompare([]byte(token), []byte(profile.APIKey)) == 1 {
			return b.contextForProfile(profile), true
		}
	}
	if b.apiKey != "" && len(token) == len(b.apiKey) && subtle.ConstantTimeCompare([]byte(token), []byte(b.apiKey)) == 1 {
		return requestContext{Config: b.cfg}, true
	}
	return requestContext{}, false
}

func (b *BridgeServer) contextWithRequestHints(ctx requestContext, r *http.Request, req ChatRequest) requestContext {
	if ctx.AppID != "" {
		return ctx
	}
	if profile, ok := b.findAppProfile(r.Header.Get("X-Bridge-App")); ok {
		return b.contextForProfile(profile)
	}
	for _, field := range []string{"bridge_app", "bridgeApp", "app", "client"} {
		if value, ok := req.Metadata[field].(string); ok {
			if profile, ok := b.findAppProfile(value); ok {
				return b.contextForProfile(profile)
			}
		}
	}
	return ctx
}

func (b *BridgeServer) findAppProfile(value string) (AppProfile, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return AppProfile{}, false
	}
	for _, profile := range b.appProfiles {
		if !profile.Enabled {
			continue
		}
		if strings.ToLower(profile.ID) == value || strings.ToLower(profile.Name) == value {
			return profile, true
		}
	}
	return AppProfile{}, false
}

func (b *BridgeServer) contextForProfile(profile AppProfile) requestContext {
	return requestContext{AppID: profile.ID, AppName: profile.Name, Config: mergeAppConfig(b.cfg, profile)}
}

func mergeAppConfig(cfg BridgeConfig, profile AppProfile) BridgeConfig {
	if profile.SessionMode != "" {
		cfg.SessionMode = profile.SessionMode
	}
	if profile.DefaultModel != "" {
		cfg.DefaultModel = profile.DefaultModel
	}
	if profile.ModelAliases != "" {
		cfg.ModelAliases = mergeJSONObjects(cfg.ModelAliases, profile.ModelAliases)
	}
	if profile.OpenCodeTools != "" {
		cfg.OpenCodeTools = profile.OpenCodeTools
	}
	return cfg
}

func mergeJSONObjects(base, override string) string {
	merged := map[string]any{}
	if base != "" {
		_ = json.Unmarshal([]byte(base), &merged)
	}
	if override != "" {
		values := map[string]any{}
		if json.Unmarshal([]byte(override), &values) == nil {
			for key, value := range values {
				merged[key] = value
			}
		}
	}
	if len(merged) == 0 {
		return ""
	}
	data, _ := json.Marshal(merged)
	return string(data)
}

func (b *BridgeServer) listModels() ([]string, error) {
	if time.Now().Before(b.modelExpiry) && b.modelCache != nil {
		return b.modelCache, nil
	}
	cmd := exec.Command(b.openCodeBin(), "models")
	setNoWindow(cmd)
	cmd.Dir = valueOr(b.cfg.ProjectDir, b.root)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for alias := range b.modelAliases() {
		seen[alias] = true
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "/") {
			seen[line] = true
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	b.modelCache = models
	b.modelExpiry = time.Now().Add(time.Minute)
	return models, nil
}

func (b *BridgeServer) ensureOpenCode() error {
	if b.cfg.OpenCodeServerURL != "" {
		return nil
	}
	if b.openCodeHealthy() {
		return nil
	}
	if b.opencodeCmd == nil || b.opencodeCmd.ProcessState != nil {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, b.openCodeBin(), "serve", "--hostname", "127.0.0.1", "--port", "4096")
		setNoWindow(cmd)
		cmd.Dir = valueOr(b.cfg.ProjectDir, b.root)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			cancel()
			return err
		}
		b.opencodeCmd = cmd
		b.opencodeStop = cancel
		go b.pipeProcess(stdout, "opencode")
		go b.pipeProcess(stderr, "opencode:error")
	}
	for i := 0; i < 50; i++ {
		if b.openCodeHealthy() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("opencode server did not become healthy")
}

func (b *BridgeServer) openCodeHealthy() bool {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(b.opencodeURL + "/global/health")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (b *BridgeServer) createOpenCodeSession(prompt string) (OpenCodeSession, error) {
	title := prompt
	if len(title) > 80 {
		title = title[:80]
	}
	var result OpenCodeSession
	err := b.openCodeJSON(http.MethodPost, "/session", map[string]any{"title": valueOr(title, "OpenCode API request")}, &result)
	return result, err
}

func (b *BridgeServer) sendOpenCodeMessage(sessionID, providerID, modelID, system, prompt string, cfg BridgeConfig) (OpenCodeMessageResponse, error) {
	body := map[string]any{
		"model": map[string]string{"providerID": providerID, "modelID": modelID},
		"parts": []map[string]string{{"type": "text", "text": prompt}},
	}
	if system != "" {
		body["system"] = system
	}
	if cfg.OpenCodeAgent != "" {
		body["agent"] = cfg.OpenCodeAgent
	}
	if cfg.OpenCodeTools != "" {
		var tools map[string]any
		if json.Unmarshal([]byte(cfg.OpenCodeTools), &tools) == nil {
			body["tools"] = tools
		}
	}
	var result OpenCodeMessageResponse
	err := b.openCodeJSON(http.MethodPost, "/session/"+sessionID+"/message", body, &result)
	return result, err
}

func (b *BridgeServer) openCodeJSON(method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, b.opencodeURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("opencode %s %s failed: %d %s", method, path, resp.StatusCode, string(data))
	}
	if target != nil && len(data) > 0 {
		return json.Unmarshal(data, target)
	}
	return nil
}

func (b *BridgeServer) planSession(req ChatRequest, httpReq *http.Request, cfg BridgeConfig, appID string) sessionPlan {
	if valueOr(cfg.SessionMode, "stateful") == "stateless" {
		return sessionPlan{Messages: req.Messages, Action: "stateless"}
	}
	key, source, explicit := conversationKey(req, httpReq)
	if appID != "" {
		key = "app:" + appID + ":" + key
		source = "app." + appID + ":" + source
	}
	b.mu.Lock()
	entry, exists := b.sessions[key]
	b.mu.Unlock()
	fingerprints := messageFingerprints(req.Messages)
	if !exists {
		return sessionPlan{Key: key, KeySource: source, Messages: req.Messages, Action: "create"}
	}
	if startsWith(fingerprints, entry.Fingerprints) {
		delta := req.Messages[len(entry.Fingerprints):]
		if len(delta) == 0 && entry.LastResponse != nil {
			return sessionPlan{Key: key, KeySource: source, Entry: &entry, Messages: nil, Action: "cached", CachedResponse: entry.LastResponse}
		}
		return sessionPlan{Key: key, KeySource: source, Entry: &entry, Messages: delta, Action: "append"}
	}
	if explicit && len(req.Messages) == 1 && isUserLike(req.Messages[0]) {
		return sessionPlan{Key: key, KeySource: source, Entry: &entry, Messages: req.Messages, Action: "append-single"}
	}
	return sessionPlan{Key: key, KeySource: source, Entry: &entry, Messages: req.Messages, Action: "replace"}
}

func (b *BridgeServer) updateSession(key, source, sessionID, model string, messages []ChatMessage, content string, tokens *UsageTokens) {
	fingerprints := messageFingerprints(messages)
	fingerprints = append(fingerprints, fingerprintMessage(ChatMessage{Role: "assistant", Content: content}))
	b.mu.Lock()
	entry := b.sessions[key]
	entry.Key = key
	entry.KeySource = source
	entry.SessionID = sessionID
	entry.Model = model
	entry.Fingerprints = fingerprints
	entry.MessageCount = len(fingerprints)
	entry.LastResponse = &CachedReply{Content: content, Tokens: tokens}
	entry.UpdatedAt = time.Now().Format(time.RFC3339)
	b.sessions[key] = entry
	b.mu.Unlock()
	b.saveSessions()
}

func (b *BridgeServer) loadSessions() {
	path := b.sessionStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var store sessionStoreFile
	if json.Unmarshal(data, &store) == nil && store.Sessions != nil {
		b.sessions = store.Sessions
	}
}

func (b *BridgeServer) saveSessions() {
	if valueOr(b.cfg.SessionMode, "stateful") == "stateless" {
		return
	}
	b.mu.Lock()
	store := sessionStoreFile{Sessions: b.sessions}
	b.mu.Unlock()
	data, _ := json.MarshalIndent(store, "", "  ")
	_ = os.WriteFile(b.sessionStorePath(), append(data, '\n'), 0644)
}

func (b *BridgeServer) sessionStorePath() string {
	return filepath.Join(b.root, "bridge-sessions.json")
}

func (b *BridgeServer) modelAliases() map[string]string {
	return modelAliases(b.cfg)
}

func modelAliases(cfg BridgeConfig) map[string]string {
	aliases := map[string]string{}
	if cfg.ModelAliases != "" {
		_ = json.Unmarshal([]byte(cfg.ModelAliases), &aliases)
	}
	return aliases
}

func (b *BridgeServer) openCodeBin() string {
	return valueOr(b.cfg.OpenCodeBin, defaultOpenCodeBin())
}

func (b *BridgeServer) pipeProcess(reader io.Reader, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		b.log("[" + prefix + "] " + scanner.Text())
	}
}

func (b *BridgeServer) writeDebug(requestID, phase string, value any) {
	if b.cfg.LogDir == "" {
		return
	}
	dir := b.cfg.LogDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.root, dir)
	}
	_ = os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(value, "", "  ")
	name := fmt.Sprintf("%s-%s-%s.json", time.Now().UTC().Format("2006-01-02T15-04-05-000Z"), requestID, phase)
	_ = os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0644)
}

func (b *BridgeServer) writeJSON(w http.ResponseWriter, status int, value any) {
	b.writeCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (b *BridgeServer) writeError(w http.ResponseWriter, err error) {
	b.writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error(), "type": "bridge_error"}})
}

func (b *BridgeServer) writeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "authorization,content-type,x-bridge-session,x-bridge-app")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
}

func convertMessages(messages []ChatMessage) (string, string) {
	system := []string{}
	transcript := []string{}
	for _, msg := range messages {
		text := extractMessageText(msg.Content)
		if text == "" {
			continue
		}
		switch msg.Role {
		case "system", "developer":
			system = append(system, text)
		case "assistant":
			transcript = append(transcript, "Assistant: "+text)
		case "tool":
			transcript = append(transcript, "Tool: "+text)
		default:
			transcript = append(transcript, "User: "+text)
		}
	}
	return strings.Join(system, "\n\n"), strings.Join(transcript, "\n\n")
}

func extractMessageText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := []string{}
		for _, item := range value {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
				continue
			}
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"text", "input_text"} {
				if text, ok := obj[key].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func extractOpenCodeText(parts []OpenCodePart) string {
	texts := []string{}
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func normalizeResponseContent(content, prompt string) string {
	cleaned := strings.TrimSpace(content)
	if strings.Contains(cleaned, "\n\n") || len(cleaned) > 72 || !looksLikeCommitPrompt(prompt) {
		return cleaned
	}
	return cleaned + "\n\n"
}

func looksLikeCommitPrompt(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "commit message") || strings.Contains(lower, "staged changes") || strings.Contains(lower, "diff --git")
}

func buildChatResponse(id, model, content, finish string, tokens *UsageTokens) map[string]any {
	usage := map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
	if tokens != nil {
		usage["prompt_tokens"] = tokens.Input
		usage["completion_tokens"] = tokens.Output
		usage["total_tokens"] = tokens.Input + tokens.Output
		if usage["total_tokens"] == 0 {
			usage["total_tokens"] = tokens.Total
		}
	}
	return map[string]any{"id": "chatcmpl-" + valueOr(id, shortHash(time.Now().String())), "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": finish}}, "usage": usage}
}

func conversationKey(req ChatRequest, httpReq *http.Request) (string, string, bool) {
	if header := httpReq.Header.Get("X-Bridge-Session"); header != "" {
		return "header:" + header, "x-bridge-session", true
	}
	for _, field := range []string{"conversation_id", "conversationId", "session_id", "sessionId", "thread_id", "threadId"} {
		if value, ok := req.Metadata[field].(string); ok && value != "" {
			return "metadata:" + value, "metadata." + field, true
		}
	}
	if req.User != "" {
		return "user:" + req.User, "user", true
	}
	system := []string{}
	firstUser := ""
	for _, msg := range req.Messages {
		if msg.Role == "system" || msg.Role == "developer" {
			system = append(system, extractMessageText(msg.Content))
		}
		if firstUser == "" && isUserLike(msg) {
			firstUser = extractMessageText(msg.Content)
		}
	}
	return "auto:" + shortHash(strings.Join(system, "\n")+"\n---\n"+firstUser), "auto", false
}

func messageFingerprints(messages []ChatMessage) []string {
	result := make([]string, 0, len(messages))
	for _, msg := range messages {
		result = append(result, fingerprintMessage(msg))
	}
	return result
}

func fingerprintMessage(msg ChatMessage) string {
	return shortHash(valueOr(msg.Role, "user") + "\n" + extractMessageText(msg.Content))
}

func startsWith(value, prefix []string) bool {
	if len(prefix) > len(value) {
		return false
	}
	for index := range prefix {
		if value[index] != prefix[index] {
			return false
		}
	}
	return true
}

func isUserLike(msg ChatMessage) bool { return msg.Role == "" || msg.Role == "user" }

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:32]
}

func maybeLogBody(value string, full bool) string {
	if full || len(value) <= 500 {
		return value
	}
	return value[:500] + "..."
}

func defaultOpenCodeBin() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		candidates := []string{
			filepath.Join(appdata, "npm", "node_modules", "opencode-ai", "bin", "opencode.exe"),
			filepath.Join(appdata, "npm", "opencode.cmd"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	if _, err := exec.LookPath("opencode"); err == nil {
		return "opencode"
	}
	if _, err := exec.LookPath("opencode.cmd"); err == nil {
		return "opencode.cmd"
	}
	return "opencode"
}
