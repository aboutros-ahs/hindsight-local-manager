package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type UpdateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	HasUpdate       bool   `json:"hasUpdate"`
	State           string `json:"state"`
	Message         string `json:"message"`
	Progress        int    `json:"progress"`
	AssetName       string `json:"assetName"`
	ReleaseURL      string `json:"releaseUrl"`
	DownloadPath    string `json:"downloadPath"`
	TokenConfigured bool   `json:"tokenConfigured"`
	assetAPIURL     string
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	HTMLURL string        `json:"html_url"`
	Draft   bool          `json:"draft"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

func (a *App) GetUpdateStatus() UpdateStatus {
	a.updateMu.Lock()
	status := a.updateStatus
	a.updateMu.Unlock()
	if status.State == "" {
		status.State = "idle"
	}
	status.CurrentVersion = appVersion
	status.TokenConfigured = a.githubTokenConfigured()
	return status
}

func (a *App) SaveUpdateSettings(repo, token string, checkOnLaunch bool) error {
	repo = strings.TrimSpace(repo)
	if repo != "" && !validGitHubRepo(repo) {
		return errors.New("GitHub repo must look like owner/repo")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.Update.GitHubRepo = repo
	cfg.Update.CheckOnLaunch = checkOnLaunch
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(token) != "" {
		return a.writeGitHubToken(token)
	}
	return nil
}

func (a *App) ClearUpdateToken() error {
	path := a.githubTokenPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) CheckForUpdate() (UpdateStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return UpdateStatus{}, err
	}
	repo := strings.TrimSpace(cfg.Update.GitHubRepo)
	if !validGitHubRepo(repo) {
		return UpdateStatus{}, errors.New("set GitHub repo as owner/repo before checking for updates")
	}
	a.setUpdateStatus(UpdateStatus{CurrentVersion: appVersion, State: "checking", Message: "Checking GitHub releases...", Progress: 0})

	release, err := a.fetchLatestRelease(repo)
	if err != nil {
		status := UpdateStatus{CurrentVersion: appVersion, State: "error", Message: err.Error()}
		a.setUpdateStatus(status)
		return a.GetUpdateStatus(), err
	}
	asset, ok := selectUpdaterAsset(release.Assets)
	if !ok {
		err := errors.New("latest release has no .exe asset for the updater")
		status := UpdateStatus{CurrentVersion: appVersion, LatestVersion: release.TagName, ReleaseURL: release.HTMLURL, State: "error", Message: err.Error()}
		a.setUpdateStatus(status)
		return a.GetUpdateStatus(), err
	}
	hasUpdate := versionGreater(release.TagName, appVersion)
	state := "idle"
	message := "You are on the latest version."
	if hasUpdate {
		state = "available"
		message = "Update available."
	}
	status := UpdateStatus{
		CurrentVersion: appVersion,
		LatestVersion:  release.TagName,
		HasUpdate:      hasUpdate,
		State:          state,
		Message:        message,
		Progress:       0,
		AssetName:      asset.Name,
		ReleaseURL:     release.HTMLURL,
		assetAPIURL:    asset.URL,
	}
	a.setUpdateStatus(status)
	return a.GetUpdateStatus(), nil
}

func (a *App) DownloadUpdate() (UpdateStatus, error) {
	a.updateMu.Lock()
	status := a.updateStatus
	a.updateMu.Unlock()
	if !status.HasUpdate || status.assetAPIURL == "" {
		return a.GetUpdateStatus(), errors.New("check for an available update before downloading")
	}
	if status.State == "downloading" {
		return a.GetUpdateStatus(), nil
	}
	status.State = "downloading"
	status.Message = "Downloading update..."
	status.Progress = 0
	status.DownloadPath = ""
	a.setUpdateStatus(status)
	go a.downloadUpdateAsset(status)
	return a.GetUpdateStatus(), nil
}

func (a *App) InstallDownloadedUpdate() error {
	status := a.GetUpdateStatus()
	if status.State != "downloaded" || strings.TrimSpace(status.DownloadPath) == "" {
		return errors.New("download an update before installing")
	}
	if !strings.EqualFold(filepath.Ext(status.DownloadPath), ".exe") {
		return errors.New("downloaded update is not an executable")
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(a.data, "updates", "apply-update.ps1")
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$pidToWait = %d
$source = '%s'
$target = '%s'
while (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 300 }
Copy-Item -LiteralPath $source -Destination $target -Force
Start-Process -FilePath $target
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force
`, os.Getpid(), psSingleQuote(status.DownloadPath), psSingleQuote(currentExe))
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	setNoWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	a.forceQuit = true
	wailsRuntime.Quit(a.ctx)
	return nil
}

func (a *App) fetchLatestRelease(repo string) (githubRelease, error) {
	endpoint := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	a.applyGitHubHeaders(req, "application/vnd.github+json")
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return githubRelease{}, errors.New("release not found; for private repos set a GitHub token with repo read access")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return githubRelease{}, fmt.Errorf("GitHub release check failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.TagName == "" {
		return githubRelease{}, errors.New("latest release did not include a version tag")
	}
	return release, nil
}

func (a *App) downloadUpdateAsset(status UpdateStatus) {
	path := filepath.Join(a.data, "updates", safePathSegment(status.LatestVersion), status.AssetName)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		a.setUpdateError(err)
		return
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, status.assetAPIURL, nil)
	if err != nil {
		a.setUpdateError(err)
		return
	}
	a.applyGitHubHeaders(req, "application/octet-stream")
	client := http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		a.setUpdateError(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		a.setUpdateError(fmt.Errorf("update download failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data))))
		return
	}
	out, err := os.Create(path)
	if err != nil {
		a.setUpdateError(err)
		return
	}
	defer out.Close()

	buf := make([]byte, 256*1024)
	var written int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				a.setUpdateError(err)
				return
			}
			written += int64(n)
			progress := 0
			if resp.ContentLength > 0 {
				progress = int(float64(written) / float64(resp.ContentLength) * 100)
			}
			a.updateMu.Lock()
			current := a.updateStatus
			current.State = "downloading"
			current.Message = "Downloading update..."
			current.Progress = progress
			a.updateStatus = current
			a.updateMu.Unlock()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			a.setUpdateError(readErr)
			return
		}
	}
	status.State = "downloaded"
	status.Message = "Update downloaded. Restart to install."
	status.Progress = 100
	status.DownloadPath = path
	a.setUpdateStatus(status)
	a.appendLog("update downloaded: " + path)
}

func (a *App) applyGitHubHeaders(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := a.readGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (a *App) readGitHubToken() string {
	if data, err := os.ReadFile(a.githubTokenPath()); err == nil {
		return strings.TrimSpace(string(data))
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func (a *App) writeGitHubToken(token string) error {
	path := a.githubTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0600)
}

func (a *App) githubTokenConfigured() bool { return a.readGitHubToken() != "" }

func (a *App) githubTokenPath() string {
	return filepath.Join(a.data, "secrets", "github-release-token")
}

func (a *App) setUpdateStatus(status UpdateStatus) {
	if status.CurrentVersion == "" {
		status.CurrentVersion = appVersion
	}
	a.updateMu.Lock()
	a.updateStatus = status
	a.updateMu.Unlock()
}

func (a *App) setUpdateError(err error) {
	a.updateMu.Lock()
	status := a.updateStatus
	status.CurrentVersion = appVersion
	status.State = "error"
	status.Message = err.Error()
	a.updateStatus = status
	a.updateMu.Unlock()
	a.appendLog("update failed: " + err.Error())
}

func validGitHubRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" && !strings.Contains(repo, " ")
}

func selectUpdaterAsset(assets []githubAsset) (githubAsset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, "Hindsight Local Manager.exe") {
			return asset, true
		}
	}
	for _, asset := range assets {
		if strings.EqualFold(filepath.Ext(asset.Name), ".exe") && !strings.Contains(strings.ToLower(asset.Name), "installer") {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func versionGreater(latest, current string) bool {
	latestParts := semverParts(latest)
	currentParts := semverParts(current)
	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

func semverParts(version string) [3]int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.Split(version, "-")[0]
	pieces := strings.Split(version, ".")
	var parts [3]int
	for i := 0; i < len(pieces) && i < 3; i++ {
		value, _ := strconv.Atoi(pieces[i])
		parts[i] = value
	}
	return parts
}

func safePathSegment(value string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	return replacer.Replace(valueOr(value, "update"))
}

func psSingleQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }
