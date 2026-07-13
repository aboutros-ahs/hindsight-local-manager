package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	appName              = "Hindsight Local Manager"
	appVersionFallback   = "0.1.0"
	runtimeRootFile      = ".runtime-root"
	defaultBridgeHost    = "127.0.0.1"
	defaultBridgePort    = "17331"
	defaultOpenCodePort  = "4096"
	defaultHindsightHost = "127.0.0.1"
	defaultHindsightPort = "8888"
	defaultUIPort        = "9999"
	defaultBankID        = "default"
	defaultUpdateRepo    = "aboutros-ahs/hindsight-local-manager"
)

var appVersion = appVersionFallback

type App struct {
	ctx    context.Context
	root   string
	data   string
	bridge *BridgeServer
	tray   *TrayManager

	mu           sync.Mutex
	logs         []string
	hindsight    *managedProcess
	controlPlane *managedProcess
	forceQuit    bool
	updateMu     sync.Mutex
	updateStatus UpdateStatus
	setupMu      sync.Mutex
	setupStatus  SetupStatus
}

type BridgeConfig struct {
	Host              string `json:"host"`
	Port              string `json:"port"`
	ProjectDir        string `json:"projectDir"`
	DefaultModel      string `json:"defaultModel"`
	OpenCodeBin       string `json:"openCodeBin"`
	SessionMode       string `json:"sessionMode"`
	LogDir            string `json:"logDir"`
	LogBodies         bool   `json:"logBodies"`
	CloseToTray       bool   `json:"closeToTray"`
	MinimizeToTray    bool   `json:"minimizeToTray"`
	ModelAliases      string `json:"modelAliases"`
	OpenCodeServerURL string `json:"openCodeServerUrl"`
	OpenCodeAgent     string `json:"openCodeAgent"`
	OpenCodeTools     string `json:"openCodeTools"`
}

type AppProfile struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	APIKey        string `json:"apiKey"`
	SessionMode   string `json:"sessionMode"`
	DefaultModel  string `json:"defaultModel"`
	ModelAliases  string `json:"modelAliases"`
	OpenCodeTools string `json:"openCodeTools"`
}

type ManagerConfig struct {
	Bridge                BridgeConfig `json:"bridge"`
	Update                UpdateConfig `json:"update"`
	StartServicesOnLaunch bool         `json:"startServicesOnLaunch"`
	StartUIOnLaunch       bool         `json:"startUiOnLaunch"`
	OpenUIBrowserOnLaunch bool         `json:"openUiBrowserOnLaunch"`
	HindsightHost         string       `json:"hindsightHost"`
	HindsightPort         string       `json:"hindsightPort"`
	ControlPlanePort      string       `json:"controlPlanePort"`
	DynamicBankIDs        bool         `json:"dynamicBankIds"`
	Autostart             bool         `json:"autostart"`
	Debug                 bool         `json:"debug"`
}

type UpdateConfig struct {
	GitHubRepo    string `json:"githubRepo"`
	CheckOnLaunch bool   `json:"checkOnLaunch"`
}

type ServiceStatus struct {
	Running bool   `json:"running"`
	Healthy bool   `json:"healthy"`
	URL     string `json:"url"`
	Detail  string `json:"detail"`
}

type IntegrationStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Detail    string `json:"detail"`
}

type SetupStatus struct {
	Active  bool        `json:"active"`
	Title   string      `json:"title"`
	Message string      `json:"message"`
	Steps   []SetupStep `json:"steps"`
}

type SetupStep struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Progress int    `json:"progress"`
	Detail   string `json:"detail"`
}

type OpenCodeConfigChoice struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type ManagerStatus struct {
	Config       ManagerConfig     `json:"config"`
	OpenCode     ServiceStatus     `json:"openCode"`
	Bridge       ServiceStatus     `json:"bridge"`
	Hindsight    ServiceStatus     `json:"hindsight"`
	MCP          ServiceStatus     `json:"mcp"`
	ControlPlane ServiceStatus     `json:"controlPlane"`
	OpenCodePlug IntegrationStatus `json:"openCodePlugin"`
	OpenCodeMCP  IntegrationStatus `json:"openCodeMcp"`
	CodexHooks   IntegrationStatus `json:"codexHooks"`
	APIKey       string            `json:"apiKey"`
	Models       []string          `json:"models"`
	LogTail      []string          `json:"logTail"`
	Paths        map[string]string `json:"paths"`
	Version      string            `json:"version"`
	Update       UpdateStatus      `json:"update"`
	Setup        SetupStatus       `json:"setup"`
	LastUpdated  string            `json:"lastUpdated"`
}

type managedProcess struct {
	name   string
	cmd    *exec.Cmd
	cancel context.CancelFunc
	url    string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.root = detectInstallDir()
	a.data = appDataDir()
	_ = os.MkdirAll(a.data, 0700)
	if a.tray != nil {
		a.tray.Start()
	}
	go func() {
		if err := a.StartBridge(); err != nil {
			a.appendLog("hidden bridge autostart failed: " + err.Error())
		}
	}()
	cfg, err := a.LoadConfig()
	if err == nil && cfg.StartServicesOnLaunch {
		go func() {
			if err := a.startLaunchServices(cfg); err != nil {
				a.appendLog("startup failed: " + err.Error())
			}
		}()
	}
	if err == nil && cfg.Update.CheckOnLaunch && strings.TrimSpace(cfg.Update.GitHubRepo) != "" {
		go func() {
			if _, err := a.CheckForUpdate(); err != nil {
				a.appendLog("update check failed: " + err.Error())
			}
		}()
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.tray != nil {
		a.tray.Stop()
	}
	_ = a.StopAll()
	_ = a.StopBridge()
}

func (a *App) SetTrayManager(tray *TrayManager) { a.tray = tray }

func (a *App) LoadConfig() (ManagerConfig, error) {
	path := a.configPath()
	data, err := os.ReadFile(path)
	if err == nil {
		var cfg ManagerConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return ManagerConfig{}, err
		}
		return withManagerDefaults(cfg, a.data), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ManagerConfig{}, err
	}
	return withManagerDefaults(ManagerConfig{}, a.data), nil
}

func (a *App) SaveConfig(cfg ManagerConfig) error {
	cfg = withManagerDefaults(cfg, a.data)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.configPath()), 0700); err != nil {
		return err
	}
	return os.WriteFile(a.configPath(), append(data, '\n'), 0600)
}

func (a *App) GetStatus() (ManagerStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ManagerStatus{}, err
	}
	key, _ := a.GetAPIKey()
	bridgeBase := fmt.Sprintf("http://%s:%s", valueOr(cfg.Bridge.Host, defaultBridgeHost), valueOr(cfg.Bridge.Port, defaultBridgePort))
	hindsightBase := fmt.Sprintf("http://%s:%s", valueOr(cfg.HindsightHost, defaultHindsightHost), valueOr(cfg.HindsightPort, defaultHindsightPort))
	uiBase := fmt.Sprintf("http://127.0.0.1:%s", valueOr(cfg.ControlPlanePort, defaultUIPort))
	hindsightProcessRunning := a.processRunning(a.hindsight)
	hindsightHealthy := checkHealth(hindsightBase + "/health")
	mcpHealthy := checkMCP(hindsightBase + "/mcp/")
	uiProcessRunning := a.processRunning(a.controlPlane)
	uiHealthy := checkHealth(uiBase)
	uiDetail := a.controlPlaneCommandDescription()
	if !uiProcessRunning && !uiHealthy {
		if detail, inUse := portConflictDetail(valueOr(cfg.ControlPlanePort, defaultUIPort)); inUse {
			uiDetail = detail
		}
	}
	openCodeHealthy := checkHealth(openCodeURL(cfg.Bridge) + "/global/health")
	return ManagerStatus{
		Config: cfg,
		OpenCode: ServiceStatus{
			Running: openCodeHealthy,
			Healthy: openCodeHealthy,
			URL:     openCodeURL(cfg.Bridge),
			Detail:  defaultOpenCodeBin(),
		},
		Bridge: ServiceStatus{
			Running: a.bridge != nil && a.bridge.Running(),
			Healthy: checkHealth(bridgeBase + "/health"),
			URL:     bridgeBase + "/v1",
			Detail:  "hidden Hindsight-only OpenAI-compatible bridge",
		},
		Hindsight: ServiceStatus{
			Running: hindsightProcessRunning || hindsightHealthy,
			Healthy: hindsightHealthy,
			URL:     hindsightBase,
			Detail:  a.hindsightCommandDescription(),
		},
		MCP: ServiceStatus{
			Running: hindsightProcessRunning || hindsightHealthy || mcpHealthy,
			Healthy: mcpHealthy,
			URL:     hindsightBase + "/mcp/",
			Detail:  "HTTP MCP endpoint",
		},
		ControlPlane: ServiceStatus{
			Running: uiProcessRunning || uiHealthy,
			Healthy: uiHealthy,
			URL:     uiBase,
			Detail:  uiDetail,
		},
		OpenCodePlug: inspectOpenCodePlugin(),
		OpenCodeMCP:  inspectOpenCodeMCP(),
		CodexHooks:   inspectCodexHooks(),
		APIKey:       key,
		Models:       []string{},
		LogTail:      a.logTail(160),
		Paths: map[string]string{
			"install": a.root,
			"data":    a.data,
			"config":  a.configPath(),
		},
		Version:     appVersion,
		Update:      a.GetUpdateStatus(),
		Setup:       a.GetSetupStatus(),
		LastUpdated: time.Now().Format(time.RFC3339),
	}, nil
}

func (a *App) StartAll() error {
	a.startSetup("Starting Hindsight services", []SetupStep{
		{Name: "Hidden bridge", State: "pending", Progress: 0, Detail: "Waiting to start"},
		{Name: "Embedding model", State: "pending", Progress: 0, Detail: "Checking local cache"},
		{Name: "Hindsight API", State: "pending", Progress: 0, Detail: "Waiting to start"},
		{Name: "Hindsight UI", State: "pending", Progress: 0, Detail: "Waiting for API"},
	})
	a.updateSetupStep("Hidden bridge", "running", 20, "Starting hidden bridge")
	if err := a.StartBridge(); err != nil {
		a.failSetup("Hidden bridge", err)
		return err
	}
	a.updateSetupStep("Hidden bridge", "complete", 100, "Bridge ready")
	if err := a.StartHindsight(); err != nil {
		a.failSetup("Hindsight API", err)
		return err
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if err := a.waitForHindsightReady(cfg, 60*time.Second); err != nil {
		a.failSetup("Hindsight API", err)
		return err
	}
	a.updateSetupStep("Hindsight API", "complete", 100, "API healthy")
	if err := a.EnsureDefaultMemoryBank(); err != nil {
		a.appendLog("default memory bank setup failed: " + err.Error())
	}
	a.updateSetupStep("Hindsight UI", "running", 40, "Starting UI")
	if err := a.StartControlPlane(); err != nil {
		a.failSetup("Hindsight UI", err)
		return err
	}
	a.updateSetupStep("Hindsight UI", "complete", 100, "UI ready")
	a.finishSetup("Services ready")
	return nil
}

func (a *App) startLaunchServices(cfg ManagerConfig) error {
	if err := a.StartBridge(); err != nil {
		return err
	}
	if err := a.StartHindsight(); err != nil {
		return err
	}
	if err := a.waitForHindsightReady(cfg, 60*time.Second); err != nil {
		return err
	}
	if err := a.EnsureDefaultMemoryBank(); err != nil {
		a.appendLog("default memory bank setup failed: " + err.Error())
	}
	if cfg.StartUIOnLaunch {
		if err := a.StartControlPlane(); err != nil {
			return err
		}
		if cfg.OpenUIBrowserOnLaunch {
			_ = a.OpenControlPlane()
		}
	}
	return nil
}

func (a *App) StopAll() error {
	_ = a.StopControlPlane()
	return a.StopHindsight()
}

func (a *App) StartBridge() error {
	if a.bridge != nil && a.bridge.Running() {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if checkHealth(a.hindsightURL(cfg) + "/health") {
		a.appendLog("hindsight already available at " + a.hindsightURL(cfg))
		go a.ensureDefaultMemoryBankWhenReady(cfg)
		return nil
	}
	key, err := a.GetAPIKey()
	if err != nil {
		return err
	}
	bridge, err := NewBridgeServer(a.data, cfg.Bridge, key, nil, a.appendLog)
	if err != nil {
		return err
	}
	if err := bridge.Start(); err != nil {
		return err
	}
	a.bridge = bridge
	return nil
}

func (a *App) StopBridge() error {
	if a.bridge == nil {
		return nil
	}
	err := a.bridge.Stop(context.Background())
	a.bridge = nil
	return err
}

func (a *App) StartHindsight() error {
	startedSetup := false
	if !a.setupActive() {
		startedSetup = true
		a.startSetup("Starting Hindsight API", []SetupStep{
			{Name: "Hidden bridge", State: "pending", Progress: 0, Detail: "Waiting to start"},
			{Name: "Embedding model", State: "pending", Progress: 0, Detail: "Checking local cache"},
			{Name: "Hindsight API", State: "pending", Progress: 0, Detail: "Waiting to start"},
		})
	}
	if a.processRunning(a.hindsight) {
		a.updateSetupStep("Hindsight API", "complete", 100, "Already running")
		if startedSetup {
			a.finishSetup("Hindsight API already running")
		}
		return nil
	}
	a.updateSetupStep("Hidden bridge", "running", 20, "Starting hidden bridge")
	if err := a.StartBridge(); err != nil {
		a.failSetup("Hidden bridge", err)
		return err
	}
	a.updateSetupStep("Hidden bridge", "complete", 100, "Bridge ready")
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	key, err := a.GetAPIKey()
	if err != nil {
		return err
	}
	exe, args, err := a.hindsightCommand(cfg)
	if err != nil {
		a.failSetup("Hindsight API", err)
		return err
	}
	if err := a.ensureEmbeddingModel(cfg, exe); err != nil {
		a.failSetup("Embedding model", err)
		return err
	}
	a.updateSetupStep("Hindsight API", "running", 50, "Launching API process")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, exe, args...)
	setNoWindow(cmd)
	cmd.Dir = a.data
	cmd.Env = append(os.Environ(), a.hindsightEnv(cfg, key)...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		a.failSetup("Hindsight API", err)
		return err
	}
	a.hindsight = &managedProcess{name: "hindsight", cmd: cmd, cancel: cancel, url: a.hindsightURL(cfg)}
	go a.pipeProcess(stdout, "hindsight")
	go a.pipeProcess(stderr, "hindsight:error")
	go a.waitProcess(a.hindsight)
	a.appendLog("hindsight starting at " + a.hindsightURL(cfg))
	go a.ensureDefaultMemoryBankWhenReady(cfg)
	if startedSetup {
		go func() {
			if err := a.waitForHindsightReady(cfg, 60*time.Second); err != nil {
				a.failSetup("Hindsight API", err)
				return
			}
			a.updateSetupStep("Hindsight API", "complete", 100, "API healthy")
			a.finishSetup("Hindsight API ready")
		}()
	}
	return nil
}

func (a *App) StopHindsight() error {
	_ = a.StopControlPlane()
	err := a.stopManaged(&a.hindsight, "hindsight")
	if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
		a.stopPortProcesses(valueOr(cfg.HindsightPort, defaultHindsightPort), a.isHindsightProcess, "hindsight")
	}
	return err
}

func (a *App) StartControlPlane() error {
	startedSetup := false
	if !a.setupActive() {
		startedSetup = true
		a.startSetup("Starting Hindsight UI", []SetupStep{{Name: "Hindsight UI", State: "pending", Progress: 0, Detail: "Waiting to start"}})
	}
	if a.processRunning(a.controlPlane) {
		a.updateSetupStep("Hindsight UI", "complete", 100, "Already running")
		if startedSetup {
			a.finishSetup("Hindsight UI already running")
		}
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !checkHealth(a.hindsightURL(cfg) + "/health") {
		a.failSetup("Hindsight UI", errors.New("start Hindsight API before starting Hindsight UI"))
		return errors.New("start Hindsight API before starting Hindsight UI")
	}
	uiURL := "http://127.0.0.1:" + valueOr(cfg.ControlPlanePort, defaultUIPort)
	if checkHealth(uiURL) {
		a.appendLog("hindsight ui already available at " + uiURL)
		a.updateSetupStep("Hindsight UI", "complete", 100, "Already available")
		if startedSetup {
			a.finishSetup("Hindsight UI already available")
		}
		return nil
	}
	if detail, inUse := portConflictDetail(valueOr(cfg.ControlPlanePort, defaultUIPort)); inUse {
		a.failSetup("Hindsight UI", errors.New(detail))
		return errors.New(detail)
	}
	exe, args, err := a.controlPlaneCommand()
	if err != nil {
		a.failSetup("Hindsight UI", err)
		return err
	}
	a.updateSetupStep("Hindsight UI", "running", 60, "Launching UI process")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, exe, args...)
	setNoWindow(cmd)
	cmd.Dir = a.data
	cmd.Env = append(os.Environ(),
		"HINDSIGHT_CP_DATAPLANE_API_URL="+a.hindsightURL(cfg),
		"PORT="+valueOr(cfg.ControlPlanePort, defaultUIPort),
		"npm_config_cache="+filepath.Join(a.data, "cache", "npm"),
	)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		a.failSetup("Hindsight UI", err)
		return err
	}
	a.controlPlane = &managedProcess{name: "hindsight-ui", cmd: cmd, cancel: cancel, url: uiURL}
	go a.pipeProcess(stdout, "hindsight-ui")
	go a.pipeProcess(stderr, "hindsight-ui:error")
	go a.waitProcess(a.controlPlane)
	a.appendLog("hindsight ui starting at " + a.controlPlane.url)
	if startedSetup {
		a.updateSetupStep("Hindsight UI", "complete", 100, "UI started")
		a.finishSetup("Hindsight UI ready")
	}
	return nil
}

func (a *App) StopControlPlane() error {
	err := a.stopManaged(&a.controlPlane, "hindsight-ui")
	if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
		a.stopPortProcesses(valueOr(cfg.ControlPlanePort, defaultUIPort), a.isControlPlaneProcess, "hindsight-ui")
	}
	return err
}

func (a *App) OpenControlPlane() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	return openURL("http://127.0.0.1:" + valueOr(cfg.ControlPlanePort, defaultUIPort))
}

func (a *App) ListOpenCodeModels(cfg BridgeConfig) ([]string, error) {
	cfg = withBridgeDefaults(cfg, a.data)
	bridge, err := NewBridgeServer(a.data, cfg, "status-check", nil, a.appendLog)
	if err != nil {
		return nil, err
	}
	models, err := bridge.listModels()
	if err != nil {
		return []string{}, err
	}
	return models, nil
}

func (a *App) InstallOpenCodePlugin() error {
	path, err := a.chooseOpenCodeConfigPath()
	if err != nil {
		return err
	}
	return installOpenCodePlugin(path, a.hindsightURLFromConfig(), false)
}

func (a *App) InstallOpenCodePluginAt(path string) error {
	if err := validateOpenCodeConfigPath(path); err != nil {
		return err
	}
	return installOpenCodePlugin(path, a.hindsightURLFromConfig(), false)
}

func (a *App) InstallOpenCodeMCP() error {
	path, err := a.chooseOpenCodeConfigPath()
	if err != nil {
		return err
	}
	return installOpenCodePlugin(path, a.hindsightURLFromConfig(), true)
}

func (a *App) InstallOpenCodeMCPAt(path string) error {
	if err := validateOpenCodeConfigPath(path); err != nil {
		return err
	}
	return installOpenCodePlugin(path, a.hindsightURLFromConfig(), true)
}

func (a *App) InstallCodexHooks() error { return installCodexHooks(a.root, a.hindsightURLFromConfig()) }

func (a *App) ListOpenCodeConfigChoices() []OpenCodeConfigChoice {
	jsonPath, jsoncPath := openCodeConfigPaths()
	var choices []OpenCodeConfigChoice
	if fileExists(jsoncPath) {
		choices = append(choices, OpenCodeConfigChoice{Label: "opencode.jsonc", Path: jsoncPath})
	}
	if fileExists(jsonPath) {
		choices = append(choices, OpenCodeConfigChoice{Label: "opencode.json", Path: jsonPath})
	}
	if len(choices) == 0 {
		choices = append(choices, OpenCodeConfigChoice{Label: "opencode.jsonc", Path: jsoncPath})
	}
	return choices
}

func (a *App) chooseOpenCodeConfigPath() (string, error) {
	jsonPath, jsoncPath := openCodeConfigPaths()
	jsonExists := fileExists(jsonPath)
	jsoncExists := fileExists(jsoncPath)
	if jsoncExists {
		return jsoncPath, nil
	}
	if jsonExists {
		return jsonPath, nil
	}
	return jsoncPath, nil
}

func (a *App) EnsureDefaultMemoryBank() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !checkHealth(a.hindsightURL(cfg) + "/health") {
		return errors.New("start Hindsight API before creating the default memory bank")
	}
	if err := a.setBankConfig(cfg, defaultBankID, "Default long-term memory bank for Hindsight Local Manager.", "Retain durable facts, decisions, preferences, and useful project context."); err != nil {
		return err
	}
	a.appendLog("default memory bank ready: " + defaultBankID)
	return nil
}

func (a *App) GetAPIKey() (string, error) {
	path := filepath.Join(a.data, "secrets", "bridge-api-key")
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	key, err := randomKey("hlm_")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	return key, os.WriteFile(path, []byte(key+"\n"), 0600)
}

func (a *App) CopyText(value string) error {
	wailsRuntime.ClipboardSetText(a.ctx, value)
	return nil
}

func (a *App) ShowWindow() {
	go func() {
		wailsRuntime.WindowShow(a.ctx)
		wailsRuntime.WindowUnminimise(a.ctx)
	}()
}

func (a *App) HideWindow() { wailsRuntime.WindowHide(a.ctx) }

func (a *App) HideToTray() error {
	if a.tray == nil || !a.tray.Ready() {
		return errors.New("system tray is not ready; leave the window open or use Quit App")
	}
	wailsRuntime.WindowHide(a.ctx)
	return nil
}

func (a *App) QuitApp() {
	a.forceQuit = true
	wailsRuntime.Quit(a.ctx)
}

func (a *App) BeforeClose(ctx context.Context) bool {
	if a.forceQuit {
		return false
	}
	cfg, err := a.LoadConfig()
	if err == nil && cfg.Bridge.CloseToTray && a.tray != nil && a.tray.Ready() {
		wailsRuntime.WindowHide(ctx)
		return true
	}
	return false
}

func (a *App) SetAutostart(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.Autostart = enabled
	return a.SaveConfig(cfg)
}

func (a *App) processRunning(proc *managedProcess) bool {
	return proc != nil && proc.cmd != nil && proc.cmd.Process != nil && proc.cmd.ProcessState == nil
}

func (a *App) stopManaged(target **managedProcess, label string) error {
	proc := *target
	if proc == nil {
		return nil
	}
	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.cmd != nil && proc.cmd.Process != nil && proc.cmd.ProcessState == nil {
		_ = proc.cmd.Process.Kill()
	}
	*target = nil
	a.appendLog(label + " stopped")
	return nil
}

func (a *App) stopPortProcesses(port string, matches func(string) bool, label string) {
	for _, pid := range tcpListenPIDs(port) {
		if pid == fmt.Sprint(os.Getpid()) {
			continue
		}
		commandLine := commandLineForPID(pid)
		if commandLine == "" || !matches(commandLine) {
			continue
		}
		if err := killProcessTree(pid); err != nil {
			a.appendLog(fmt.Sprintf("%s orphan PID %s stop failed: %s", label, pid, err.Error()))
			continue
		}
		a.appendLog(fmt.Sprintf("%s orphan PID %s stopped", label, pid))
	}
}

func (a *App) isHindsightProcess(commandLine string) bool {
	line := strings.ToLower(commandLine)
	return strings.Contains(line, "hindsight-local-mcp") || strings.Contains(line, "hindsight_api.mcp_local")
}

func (a *App) isControlPlaneProcess(commandLine string) bool {
	line := strings.ToLower(commandLine)
	if !strings.Contains(line, "hindsight-control-plane") {
		return false
	}
	return strings.Contains(line, strings.ToLower(a.data)) || strings.Contains(line, strings.ToLower(a.root))
}

func (a *App) waitForHindsightReady(cfg ManagerConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if checkHealth(a.hindsightURL(cfg) + "/health") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("Hindsight API did not become healthy before timeout")
}

func (a *App) ensureDefaultMemoryBankWhenReady(cfg ManagerConfig) {
	if err := a.waitForHindsightReady(cfg, 60*time.Second); err != nil {
		a.appendLog("default memory bank setup skipped: " + err.Error())
		return
	}
	if err := a.EnsureDefaultMemoryBank(); err != nil {
		a.appendLog("default memory bank setup failed: " + err.Error())
	}
}

func (a *App) ensureEmbeddingModel(cfg ManagerConfig, pythonExe string) error {
	if !strings.EqualFold(filepath.Base(pythonExe), "python.exe") {
		a.updateSetupStep("Embedding model", "complete", 100, "Using PATH runtime model cache")
		return nil
	}
	marker := filepath.Join(a.modelCacheDir(), ".bge-small-en-v1.5-ready")
	if fileExists(marker) {
		a.updateSetupStep("Embedding model", "complete", 100, "Model cache ready")
		return nil
	}
	a.updateSetupStep("Embedding model", "running", 15, "Preparing local embedding model")
	if err := os.MkdirAll(a.modelCacheDir(), 0700); err != nil {
		return err
	}
	cmd := exec.Command(pythonExe, "-c", "from sentence_transformers import SentenceTransformer; SentenceTransformer('BAAI/bge-small-en-v1.5')")
	setNoWindow(cmd)
	cmd.Dir = a.data
	cmd.Env = append(os.Environ(), a.modelEnv()...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	a.updateSetupStep("Embedding model", "running", 45, "Downloading model files if missing")
	go a.pipeProcess(stdout, "model")
	go a.pipeProcess(stderr, "model:error")
	if err := cmd.Wait(); err != nil {
		return err
	}
	if err := os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)+"\n"), 0600); err != nil {
		return err
	}
	a.updateSetupStep("Embedding model", "complete", 100, "Embedding model ready")
	return nil
}

func (a *App) modelCacheDir() string { return filepath.Join(a.data, "cache", "huggingface") }

func (a *App) modelEnv() []string {
	return []string{
		"HF_HOME=" + a.modelCacheDir(),
		"SENTENCE_TRANSFORMERS_HOME=" + a.modelCacheDir(),
	}
}

func (a *App) setBankConfig(cfg ManagerConfig, bankID, reflectMission, retainMission string) error {
	body, err := json.Marshal(map[string]any{"updates": map[string]any{
		"reflect_mission": reflectMission,
		"retain_mission":  retainMission,
	}})
	if err != nil {
		return err
	}
	endpoint := a.hindsightURL(cfg) + "/v1/default/banks/" + url.PathEscape(bankID) + "/config"
	req, err := http.NewRequest(http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("default memory bank setup failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
}

func (a *App) waitProcess(proc *managedProcess) {
	if proc == nil || proc.cmd == nil {
		return
	}
	err := proc.cmd.Wait()
	if err != nil && !strings.Contains(err.Error(), "signal: killed") {
		a.appendLog(proc.name + " exited: " + err.Error())
		return
	}
	a.appendLog(proc.name + " exited")
}

func (a *App) pipeProcess(reader io.Reader, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		a.appendLog("[" + prefix + "] " + line)
		if prefix == "hindsight" || prefix == "hindsight:error" {
			a.observeHindsightSetupLog(line)
		}
	}
}

func (a *App) GetSetupStatus() SetupStatus {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	status := a.setupStatus
	status.Steps = append([]SetupStep{}, status.Steps...)
	return status
}

func (a *App) setupActive() bool {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	return a.setupStatus.Active
}

func (a *App) startSetup(title string, steps []SetupStep) {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	a.setupStatus = SetupStatus{Active: true, Title: title, Message: "Preparing local services", Steps: append([]SetupStep{}, steps...)}
}

func (a *App) finishSetup(message string) {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if !a.setupStatus.Active {
		return
	}
	for i := range a.setupStatus.Steps {
		if a.setupStatus.Steps[i].State != "error" {
			a.setupStatus.Steps[i].State = "complete"
			a.setupStatus.Steps[i].Progress = 100
		}
	}
	a.setupStatus.Message = message
	a.setupStatus.Active = false
}

func (a *App) updateSetupStep(name, state string, progress int, detail string) {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if !a.setupStatus.Active {
		return
	}
	for i := range a.setupStatus.Steps {
		if a.setupStatus.Steps[i].Name != name {
			continue
		}
		a.setupStatus.Steps[i].State = state
		a.setupStatus.Steps[i].Progress = max(0, min(100, progress))
		a.setupStatus.Steps[i].Detail = detail
		a.setupStatus.Message = detail
		return
	}
}

func (a *App) failSetup(name string, err error) {
	a.updateSetupStep(name, "error", 100, err.Error())
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	a.setupStatus.Message = err.Error()
}

func (a *App) observeHindsightSetupLog(line string) {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "download") || strings.Contains(lower, "huggingface") || strings.Contains(lower, "sentence") || strings.Contains(lower, "transformer") {
		a.updateSetupStep("Embedding model", "running", 75, "Downloading or loading model files")
	}
}

func (a *App) appendLog(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	line = time.Now().Format("15:04:05") + " " + line
	a.logs = append(a.logs, line)
	if len(a.logs) > 800 {
		a.logs = a.logs[len(a.logs)-800:]
	}
	_ = os.MkdirAll(filepath.Join(a.data, "logs"), 0700)
	_ = os.WriteFile(filepath.Join(a.data, "logs", "manager.log"), []byte(strings.Join(a.logs, "\n")+"\n"), 0600)
}

func (a *App) logTail(limit int) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.logs) <= limit {
		return append([]string{}, a.logs...)
	}
	return append([]string{}, a.logs[len(a.logs)-limit:]...)
}

func (a *App) configPath() string { return filepath.Join(a.data, "config.json") }

func (a *App) hindsightURLFromConfig() string {
	cfg, _ := a.LoadConfig()
	return a.hindsightURL(cfg)
}

func (a *App) hindsightURL(cfg ManagerConfig) string {
	return fmt.Sprintf("http://%s:%s", valueOr(cfg.HindsightHost, defaultHindsightHost), valueOr(cfg.HindsightPort, defaultHindsightPort))
}

func (a *App) hindsightEnv(cfg ManagerConfig, key string) []string {
	bridgeURL := fmt.Sprintf("http://%s:%s/v1", valueOr(cfg.Bridge.Host, defaultBridgeHost), valueOr(cfg.Bridge.Port, defaultBridgePort))
	model := valueOr(cfg.Bridge.DefaultModel, "github-copilot/gpt-5.4-mini")
	return append(a.modelEnv(),
		"PYTHONUTF8=1",
		"PYTHONIOENCODING=utf-8",
		"HINDSIGHT_API_DATABASE_URL=pg0://hindsight-local-manager",
		"HINDSIGHT_API_LLM_PROVIDER=openai",
		"HINDSIGHT_API_LLM_BASE_URL="+bridgeURL,
		"HINDSIGHT_API_LLM_API_KEY="+key,
		"HINDSIGHT_API_LLM_MODEL="+model,
		"HINDSIGHT_API_EMBEDDINGS_PROVIDER=local",
		"HINDSIGHT_API_HOST="+valueOr(cfg.HindsightHost, defaultHindsightHost),
		"HINDSIGHT_API_PORT="+valueOr(cfg.HindsightPort, defaultHindsightPort),
	)
}

func (a *App) hindsightCommand(cfg ManagerConfig) (string, []string, error) {
	bundledPython := filepath.Join(runtimeResourcesRoot(a.root), "python", "python.exe")
	if fileExists(bundledPython) {
		return bundledPython, []string{"-m", "hindsight_api.mcp_local", "--host", valueOr(cfg.HindsightHost, defaultHindsightHost), "--port", valueOr(cfg.HindsightPort, defaultHindsightPort), "--log-level", "info"}, nil
	}
	if path, err := exec.LookPath("hindsight-local-mcp"); err == nil {
		return path, []string{"--host", valueOr(cfg.HindsightHost, defaultHindsightHost), "--port", valueOr(cfg.HindsightPort, defaultHindsightPort), "--log-level", "info"}, nil
	}
	if path := firstExisting(hindsightScriptCandidates()...); path != "" {
		return path, []string{"--host", valueOr(cfg.HindsightHost, defaultHindsightHost), "--port", valueOr(cfg.HindsightPort, defaultHindsightPort), "--log-level", "info"}, nil
	}
	if python, err := exec.LookPath("python"); err == nil {
		return python, []string{"-m", "hindsight_api.mcp_local", "--host", valueOr(cfg.HindsightHost, defaultHindsightHost), "--port", valueOr(cfg.HindsightPort, defaultHindsightPort), "--log-level", "info"}, nil
	}
	return "", nil, errors.New("hindsight runtime not found; bundled resources are missing and no user-space hindsight-local-mcp/python fallback was found")
}

func (a *App) hindsightCommandDescription() string {
	if fileExists(filepath.Join(runtimeResourcesRoot(a.root), "python", "python.exe")) {
		return "bundled Python runtime"
	}
	if path := firstExisting(hindsightScriptCandidates()...); path != "" {
		return "user-space Hindsight runtime: " + path
	}
	return "PATH fallback: hindsight-local-mcp"
}

func (a *App) controlPlaneCommand() (string, []string, error) {
	resourcesRoot := runtimeResourcesRoot(a.root)
	bundledNode := filepath.Join(resourcesRoot, "node", "node.exe")
	bundledCLI := filepath.Join(resourcesRoot, "control-plane", "node_modules", "@vectorize-io", "hindsight-control-plane", "bin", "cli.js")
	if fileExists(bundledNode) && fileExists(bundledCLI) {
		return bundledNode, []string{bundledCLI}, nil
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return path, []string{"--yes", "@vectorize-io/hindsight-control-plane@latest"}, nil
	}
	return "", nil, errors.New("Hindsight UI runtime not found; bundled Node is missing and npx is not on PATH")
}

func (a *App) controlPlaneCommandDescription() string {
	if fileExists(filepath.Join(runtimeResourcesRoot(a.root), "node", "node.exe")) {
		return "bundled Node runtime"
	}
	return "PATH fallback: npx for Hindsight UI"
}

func runtimeResourcesRoot(installRoot string) string {
	if data, err := os.ReadFile(filepath.Join(installRoot, runtimeRootFile)); err == nil {
		path := strings.TrimSpace(string(data))
		if path != "" && dirExists(path) {
			return path
		}
	}
	return filepath.Join(installRoot, "resources")
}

func withManagerDefaults(cfg ManagerConfig, data string) ManagerConfig {
	cfg.Bridge = withBridgeDefaults(cfg.Bridge, data)
	cfg.Update.GitHubRepo = valueOr(cfg.Update.GitHubRepo, defaultUpdateRepo)
	cfg.HindsightHost = valueOr(cfg.HindsightHost, defaultHindsightHost)
	cfg.HindsightPort = valueOr(cfg.HindsightPort, defaultHindsightPort)
	cfg.ControlPlanePort = valueOr(cfg.ControlPlanePort, defaultUIPort)
	return cfg
}

func withBridgeDefaults(cfg BridgeConfig, data string) BridgeConfig {
	cfg.Host = valueOr(cfg.Host, defaultBridgeHost)
	cfg.Port = valueOr(cfg.Port, defaultBridgePort)
	cfg.ProjectDir = valueOr(cfg.ProjectDir, data)
	cfg.DefaultModel = valueOr(cfg.DefaultModel, "github-copilot/gpt-5.4-mini")
	cfg.OpenCodeBin = valueOr(cfg.OpenCodeBin, defaultOpenCodeBin())
	cfg.SessionMode = valueOr(cfg.SessionMode, "stateful")
	cfg.OpenCodeServerURL = strings.TrimRight(cfg.OpenCodeServerURL, "/")
	cfg.LogDir = valueOr(cfg.LogDir, filepath.Join(data, "logs", "bridge"))
	return cfg
}

func openCodeURL(cfg BridgeConfig) string {
	return strings.TrimRight(valueOr(cfg.OpenCodeServerURL, "http://127.0.0.1:"+defaultOpenCodePort), "/")
}

func checkHealth(url string) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func checkMCP(url string) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func portConflictDetail(port string) (string, bool) {
	pids := tcpListenPIDs(port)
	if len(pids) == 0 {
		return "", false
	}
	return fmt.Sprintf("port %s is already in use by PID(s) %s", port, strings.Join(pids, ", ")), true
}

func tcpListenPIDs(port string) []string {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	setNoWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var pids []string
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if !strings.HasSuffix(fields[1], ":"+port) || seen[fields[4]] {
			continue
		}
		seen[fields[4]] = true
		pids = append(pids, fields[4])
	}
	return pids
}

func commandLineForPID(pid string) string {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_Process -Filter 'ProcessId = "+pid+"').CommandLine")
	setNoWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func killProcessTree(pid string) error {
	cmd := exec.Command("taskkill", "/PID", pid, "/T", "/F")
	setNoWindow(cmd)
	return cmd.Run()
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func hindsightScriptCandidates() []string {
	var candidates []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		matches, _ := filepath.Glob(filepath.Join(appdata, "Python", "Python*", "Scripts", "hindsight-local-mcp.exe"))
		candidates = append(candidates, matches...)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		matches, _ := filepath.Glob(filepath.Join(local, "Programs", "Python", "Python*", "Scripts", "hindsight-local-mcp.exe"))
		candidates = append(candidates, matches...)
	}
	candidates = append(candidates,
		`C:\Projects\hindsight\.venv\Scripts\hindsight-local-mcp.exe`,
		`C:\Projects\hindsight\.venv\Scripts\hindsight-local-mcp`,
	)
	return candidates
}

func detectInstallDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func appDataDir() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "HindsightLocalManager")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".hindsight-local-manager")
	}
	return ".hindsight-local-manager"
}

func randomKey(prefix string) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "="), nil
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), mode)
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}
