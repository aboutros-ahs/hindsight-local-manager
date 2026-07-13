package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultRuntimeVersion = "v0.1.7"

func (a *App) runtimeInstallStatus() RuntimeInstallStatus {
	cfg := loadRuntimeConfig(a.root)
	resourcesRoot := runtimeResourcesRoot(a.root)
	version := valueOr(cfg.RuntimeVersion, defaultRuntimeVersion)
	python := runtimePythonExe(a.root)
	if python == "" {
		python = filepath.Join(resourcesRoot, "python", "python.exe")
	}
	node, cli := runtimeControlPlaneCommand(a.root)
	if node == "" && cfg.UIInstalled {
		node = filepath.Join(resourcesRoot, "node", "node.exe")
	}
	if cli == "" && cfg.UIInstalled {
		cli = filepath.Join(resourcesRoot, "control-plane", "node_modules", "@vectorize-io", "hindsight-control-plane", "bin", "cli.js")
	}

	pythonSource := valueOr(cfg.PythonSource, runtimeSourceForPath(python, resourcesRoot))
	nodeSource := valueOr(cfg.NodeSource, runtimeSourceForPath(node, resourcesRoot))
	return RuntimeInstallStatus{
		ResourcesRoot: resourcesRoot,
		Version:       version,
		Python: RuntimeComponentStatus{
			Installed: fileExists(python),
			Source:    pythonSource,
			Path:      python,
			Version:   pythonRuntimeVersion(python),
			Detail:    runtimeDetail(pythonSource, python),
		},
		Node: RuntimeComponentStatus{
			Installed: cfg.UIInstalled && fileExists(node),
			Source:    nodeSource,
			Path:      node,
			Version:   nodeRuntimeVersion(node),
			Detail:    runtimeDetail(nodeSource, node),
		},
		ControlPlane: RuntimeComponentStatus{
			Installed: cfg.UIInstalled && fileExists(cli),
			Source:    "managed",
			Path:      cli,
			Version:   version,
			Detail:    controlPlaneDetail(cfg.UIInstalled, cli),
		},
		Reranker: RuntimeComponentStatus{
			Installed: false,
			Source:    "disabled",
			Detail:    "disabled by default to reduce CPU and memory use",
		},
	}
}

func runtimeSourceForPath(path, resourcesRoot string) string {
	if path == "" {
		return "not required"
	}
	if resourcesRoot != "" {
		rel, err := filepath.Rel(resourcesRoot, path)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return "managed"
		}
	}
	return "system"
}

func runtimeDetail(source, path string) string {
	if path == "" {
		return "not required"
	}
	return strings.TrimSpace(source + " runtime: " + path)
}

func controlPlaneDetail(installed bool, path string) string {
	if !installed {
		return "not installed"
	}
	return "managed UI runtime: " + path
}

func pythonRuntimeVersion(python string) string {
	if !fileExists(python) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "--version")
	setNoWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func nodeRuntimeVersion(node string) string {
	if !fileExists(node) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--version")
	setNoWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (a *App) InstallHindsightUI() error {
	a.startSetup("Installing Hindsight UI", []SetupStep{
		{Name: "Node runtime", State: "pending", Progress: 0, Detail: "Checking system Node"},
		{Name: "Hindsight UI", State: "pending", Progress: 0, Detail: "Waiting to download"},
	})
	cfg := loadRuntimeConfig(a.root)
	resourcesRoot := runtimeResourcesRoot(a.root)
	if resourcesRoot == "" || strings.EqualFold(resourcesRoot, filepath.Join(a.root, "resources")) {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(a.root, "runtime")
		}
		resourcesRoot = filepath.Join(localAppData, "HLM", "r", "resources")
	}
	runtimeRoot := filepath.Dir(resourcesRoot)
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		a.failSetup("Hindsight UI", err)
		return err
	}
	version := valueOr(cfg.RuntimeVersion, defaultRuntimeVersion)
	node := cfg.NodeExe
	nodeSource := cfg.NodeSource
	if !validNodeRuntime(node) {
		a.updateSetupStep("Node runtime", "running", 25, "Looking for compatible system Node")
		if systemNode := findCompatibleNodeRuntime(); systemNode != "" {
			node = systemNode
			nodeSource = "system"
			a.updateSetupStep("Node runtime", "complete", 100, "Using system Node")
		} else {
			a.updateSetupStep("Node runtime", "running", 35, "Downloading managed Node")
			if err := a.installRuntimeComponent(version, "node", runtimeRoot, "Node runtime"); err != nil {
				a.failSetup("Node runtime", err)
				return err
			}
			node = filepath.Join(resourcesRoot, "node", "node.exe")
			nodeSource = "managed"
			a.updateSetupStep("Node runtime", "complete", 100, "Managed Node installed")
		}
	} else {
		nodeSource = valueOr(nodeSource, runtimeSourceForPath(node, resourcesRoot))
		a.updateSetupStep("Node runtime", "complete", 100, "Node ready")
	}

	a.updateSetupStep("Hindsight UI", "running", 20, "Downloading UI runtime")
	if err := a.installRuntimeComponent(version, "control-plane", runtimeRoot, "Hindsight UI"); err != nil {
		a.failSetup("Hindsight UI", err)
		return err
	}
	cfg.ResourcesRoot = resourcesRoot
	cfg.NodeExe = node
	cfg.NodeSource = nodeSource
	cfg.ControlPlaneCLI = filepath.Join(resourcesRoot, "control-plane", "node_modules", "@vectorize-io", "hindsight-control-plane", "bin", "cli.js")
	cfg.UIInstalled = true
	cfg.RuntimeVersion = version
	if cfg.PythonExe == "" {
		cfg.PythonExe = filepath.Join(resourcesRoot, "python", "python.exe")
	}
	if cfg.PythonSource == "" {
		cfg.PythonSource = runtimeSourceForPath(cfg.PythonExe, resourcesRoot)
	}
	if err := saveRuntimeConfig(a.root, cfg); err != nil {
		a.failSetup("Hindsight UI", err)
		return err
	}
	_ = os.WriteFile(filepath.Join(a.root, runtimeRootFile), []byte(resourcesRoot+"\n"), 0600)
	a.updateSetupStep("Hindsight UI", "complete", 100, "UI installed")
	a.finishSetup("Hindsight UI installed")
	return nil
}

func (a *App) installRuntimeComponent(version, kind, runtimeRoot, label string) error {
	asset := fmt.Sprintf("Hindsight-Local-Manager-%s-%s.zip", version, kind)
	repo := defaultUpdateRepo
	if cfg, err := a.LoadConfig(); err == nil {
		repo = valueOr(cfg.Update.GitHubRepo, defaultUpdateRepo)
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, asset)
	cachePath := filepath.Join(a.data, "runtime-downloads", version, asset)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return err
	}
	if err := a.downloadRuntimeAsset(url, cachePath, label); err != nil {
		return err
	}
	a.updateSetupStep(label, "running", 75, "Extracting")
	return extractZipInto(cachePath, runtimeRoot)
}

func (a *App) downloadRuntimeAsset(url, path, label string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("download failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if info, err := os.Stat(path); err == nil && resp.ContentLength > 0 && info.Size() == resp.ContentLength {
		a.updateSetupStep(label, "running", 60, "Using cached download")
		return nil
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 512*1024)
	var written int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if resp.ContentLength > 0 {
				progress := 20 + int(float64(written)/float64(resp.ContentLength)*50)
				a.updateSetupStep(label, "running", progress, "Downloading")
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func extractZipInto(zipPath, destination string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	for _, file := range reader.File {
		target, err := filepath.Abs(filepath.Join(root, filepath.Clean(file.Name)))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("unsafe bundle path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(out, source)
		closeErr := out.Close()
		source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func validNodeRuntime(node string) bool {
	if !fileExists(node) {
		return false
	}
	version := nodeRuntimeVersion(node)
	if !strings.HasPrefix(version, "v") {
		return false
	}
	major, err := strconv.Atoi(strings.Split(strings.TrimPrefix(version, "v"), ".")[0])
	return err == nil && major >= 20
}

func findCompatibleNodeRuntime() string {
	if path, err := exec.LookPath("node"); err == nil && validNodeRuntime(path) {
		return path
	}
	return ""
}

func saveRuntimeConfig(installRoot string, cfg RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(installRoot, runtimeConfigFile), append(data, '\n'), 0600)
}
