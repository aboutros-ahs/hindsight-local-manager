import './styles.css';
import {
  GetStatus,
  LoadConfig,
  SaveConfig,
  StartAll,
  StopAll,
  StartHindsight,
  StopHindsight,
  StartControlPlane,
  StopControlPlane,
  OpenControlPlane,
  EnsureDefaultMemoryBank,
  ListOpenCodeConfigChoices,
  InstallOpenCodePlugin,
  InstallOpenCodePluginAt,
  InstallOpenCodeMCP,
  InstallOpenCodeMCPAt,
  InstallCodexHooks,
  SaveUpdateSettings,
  ClearUpdateToken,
  CheckForUpdate,
  DownloadUpdate,
  InstallDownloadedUpdate,
  CopyText,
  HideToTray,
  QuitApp,
} from '../wailsjs/go/main/App';

const app = document.querySelector('#app');
const state = { status: null, config: null, message: 'READY', busy: false, page: 'runtime', starting: new Set(), stopping: new Set(), openCodeInstall: null };
let gridScrollTop = 0;
let logScrollTop = 0;
let logScrollHeight = 0;
let logAutoFollow = true;

async function refresh() {
  try {
    state.status = await GetStatus();
    state.config = state.status.config;
    render();
  } catch (error) {
    state.message = errorMessage(error);
    render();
  }
}

function render() {
  const existingGrid = document.querySelector('.grid');
  if (existingGrid) gridScrollTop = existingGrid.scrollTop;
  const existingLog = document.querySelector('#runtime-log');
  if (existingLog) {
    logAutoFollow = existingLog.scrollHeight - existingLog.clientHeight - existingLog.scrollTop < 24;
    logScrollTop = existingLog.scrollTop;
    logScrollHeight = existingLog.scrollHeight;
  }
  const s = state.status || {};
  const cfg = state.config || s.config || {};
  const hindsightRunning = Boolean(s.hindsight?.running || s.hindsight?.healthy);
  const uiRunning = Boolean(s.controlPlane?.running || s.controlPlane?.healthy);
  app.innerHTML = `
    <div class="shell">
      <header class="topbar">
        <div>
          <h1>Hindsight <span>Local Manager</span></h1>
        </div>
        <div class="top-actions">
          <div class="status-cluster">
            ${pill('API', s.hindsight?.healthy)}
            ${pill('UI', s.controlPlane?.healthy)}
            <button class="pill-button" data-action="hide-to-tray">HIDE TO TRAY</button>
            <button class="pill-button" data-action="quit-app">QUIT APP</button>
          </div>
          <nav class="tabs">
            <button class="${state.page === 'runtime' ? 'primary' : ''}" data-page="runtime">RUNTIME</button>
            <button class="${state.page === 'setup' ? 'primary' : ''}" data-page="setup">SETUP</button>
            <button class="${state.page === 'tools' ? 'primary' : ''}" data-page="tools">TOOLS</button>
          </nav>
        </div>
      </header>

      <main class="grid">
        ${state.page === 'setup' ? setupPage(s, cfg) : state.page === 'tools' ? toolsPage(s, cfg) : runtimePage(s, cfg, hindsightRunning, uiRunning)}
      </main>

      <footer class="footer"><span><i></i>${escapeHtml(state.message)}</span><span>${escapeHtml(s.lastUpdated || '')}</span></footer>
      ${openCodeConfigDialog()}
    </div>`;
  bind();
  const grid = document.querySelector('.grid');
  if (grid) {
    grid.scrollTop = gridScrollTop;
    grid.addEventListener('scroll', () => {
      gridScrollTop = grid.scrollTop;
    }, { passive: true });
  }
  const log = document.querySelector('#runtime-log');
  if (log) {
    if (logAutoFollow) {
      log.scrollTop = log.scrollHeight;
    } else {
      const delta = log.scrollHeight - logScrollHeight;
      log.scrollTop = Math.max(0, logScrollTop + Math.max(0, delta));
    }
    log.addEventListener('scroll', () => {
      logAutoFollow = log.scrollHeight - log.clientHeight - log.scrollTop < 24;
      logScrollTop = log.scrollTop;
      logScrollHeight = log.scrollHeight;
    }, { passive: true });
  }
}

function toolsPage(s, cfg) {
  const update = s.update || {};
  const progress = Number.isFinite(update.progress) ? update.progress : 0;
  const canDownload = update.hasUpdate && update.state !== 'downloading' && update.state !== 'downloaded';
  const canInstall = update.state === 'downloaded';
  return `
    <section class="panel hero-panel">
      <div class="panel-head">
        <p class="overline">TOOLS</p>
        <h2>App Updates</h2>
      </div>
      <div class="service-grid">
        ${dependencyCard('Current Version', update.state === 'available' ? 'UPDATE READY' : 'INSTALLED', s.version || update.currentVersion || '0.1.0', update.message || 'Configure a GitHub repo to check releases.')}
        ${dependencyCard('GitHub Token', update.tokenConfigured ? 'CONFIGURED' : 'OPTIONAL', cfg.update?.githubRepo || 'owner/repo', 'Private repos need a fine-grained token with Contents read access. The token is stored locally, not committed.')}
      </div>
      <div class="progress-wrap">
        <div class="progress-meta"><span>${escapeHtml(update.state || 'idle')}</span><span>${progress}%</span></div>
        <div class="progress-bar"><i style="width:${Math.max(0, Math.min(100, progress))}%"></i></div>
      </div>
      <div class="actions">
        <button class="primary" data-action="check-update">CHECK FOR UPDATE</button>
        <button data-action="download-update"${canDownload ? '' : ' disabled'}>DOWNLOAD UPDATE</button>
        <button data-action="install-update"${canInstall ? '' : ' disabled'}>RESTART TO INSTALL</button>
        ${update.releaseUrl ? `<button data-action="open-release">OPEN RELEASE</button>` : ''}
      </div>
    </section>

    <section class="panel metrics-panel">
      <div class="metric"><span>CURRENT</span><strong>${escapeHtml(update.currentVersion || s.version || '')}</strong></div>
      <div class="metric"><span>LATEST</span><strong>${escapeHtml(update.latestVersion || 'unknown')}</strong></div>
      <div class="metric"><span>ASSET</span><strong>${escapeHtml(update.assetName || 'none')}</strong></div>
      <div class="metric"><span>STATE</span><strong>${escapeHtml(update.state || 'idle')}</strong></div>
    </section>

    <section class="panel config-panel">
      <div class="panel-head"><p class="overline">UPDATER</p><h2>GitHub Release Source</h2></div>
      <div class="form-grid compact">
        ${input('updateRepo', 'GITHUB_REPO', cfg.update?.githubRepo || '')}
        ${passwordInput('updateToken', update.tokenConfigured ? 'TOKEN SAVED - LEAVE BLANK TO KEEP' : 'GITHUB_TOKEN_FOR_PRIVATE_REPO', '')}
      </div>
      <div class="check-grid">
        ${checkbox('updateCheckOnLaunch', 'CHECK FOR UPDATES ON APP LAUNCH', cfg.update?.checkOnLaunch)}
      </div>
      <div class="actions">
        <button class="primary" data-action="save-update-settings">SAVE UPDATE SETTINGS</button>
        <button data-action="clear-update-token"${update.tokenConfigured ? '' : ' disabled'}>CLEAR TOKEN</button>
      </div>
    </section>

    <section class="panel log-panel">
      <div class="panel-head"><p class="overline">MEMORY_BACKUP</p><h2>Export / Import</h2></div>
      <p class="muted-copy">Planned: append-only JSON export/import for the default Hindsight memory bank, with duplicate-safe source IDs and progress.</p>
    </section>`;
}

function runtimePage(s, cfg, hindsightRunning, uiRunning) {
  const apiHealthy = Boolean(s.hindsight?.healthy);
  const uiStartDisabled = !apiHealthy && !uiRunning ? ' disabled title="Start Hindsight API first"' : '';
  const services = [
    ['api', 'Hindsight API', s.hindsight, `<button data-action="${hindsightRunning ? 'stop-hindsight' : 'start-hindsight'}">${hindsightRunning ? 'STOP' : 'START'}</button>`],
    ['ui', 'Hindsight UI', s.controlPlane, `<button data-action="${uiRunning ? 'stop-ui' : 'start-ui'}"${uiRunning ? '' : uiStartDisabled}>${uiRunning ? 'STOP' : 'START'}</button><button data-action="open-ui">OPEN UI</button>`],
  ];
  return `
    <section class="panel hero-panel">
      <div class="panel-head">
        <p class="overline">RUNTIME</p>
        <h2>Managed Services</h2>
      </div>
      <div class="actions">
        <button class="primary" data-action="start-all">START ALL</button>
        <button data-action="stop-all">STOP ALL</button>
        <button data-action="refresh">REFRESH</button>
      </div>
      <div class="service-grid">
        ${services.map(([id, name, service, actions]) => serviceCard(name, service, actions, serviceLifecycle(id))).join('')}
      </div>
    </section>

    <section class="panel metrics-panel">
      <div class="metric"><span>HINDSIGHT API</span><strong>${escapeHtml(s.hindsight?.url || 'http://127.0.0.1:8888')}</strong></div>
      <div class="metric"><span>MCP ENDPOINT</span><strong>${escapeHtml(s.mcp?.url || 'http://127.0.0.1:8888/mcp/')}</strong></div>
      <div class="metric"><span>MCP STATUS</span><strong>${s.mcp?.healthy ? 'HEALTHY' : s.mcp?.running ? 'STARTING' : 'STOPPED'}</strong></div>
      <div class="metric"><span>DATA</span><strong>${escapeHtml(s.paths?.data || '')}</strong></div>
    </section>

    <section class="panel config-panel">
      <div class="panel-head"><p class="overline">SETTINGS</p><h2>Launch Behavior</h2></div>
      <div class="check-grid">
        ${checkbox('startServicesOnLaunch', 'START SERVICES ON APP LAUNCH', cfg.startServicesOnLaunch)}
        ${checkbox('startUiOnLaunch', 'START HINDSIGHT UI ON APP LAUNCH', cfg.startUiOnLaunch)}
        ${checkbox('openUiBrowserOnLaunch', 'OPEN UI IN BROWSER', cfg.openUiBrowserOnLaunch)}
        ${checkbox('closeToTray', 'CLOSE WINDOW TO TRAY', cfg.bridge?.closeToTray)}
      </div>
      <div class="form-grid compact">
        ${input('hindsightPort', 'HINDSIGHT_PORT', cfg.hindsightPort || '8888')}
        ${input('controlPlanePort', 'UI_PORT', cfg.controlPlanePort || '9999')}
      </div>
      <div class="actions">
        <button class="primary" data-action="save-config">SAVE SETTINGS</button>
      </div>
    </section>

    <section class="panel log-panel">
      <div class="panel-head split-head"><div><p class="overline">LOGS</p><h2>Runtime Log</h2></div><button data-action="copy-log">COPY LOG</button></div>
        <pre id="runtime-log">${escapeHtml((s.logTail || []).join('\n') || 'NO_LOGS')}</pre>
      </section>`;
}

function setupPage(s, cfg) {
  return `
    <section class="panel hero-panel">
      <div class="panel-head">
        <p class="overline">FIRST_RUN</p>
        <h2>Setup Checklist</h2>
      </div>
      <div class="service-grid">
        ${dependencyCard('OpenCode CLI', s.openCode?.healthy ? 'SESSION READY' : 'ON DEMAND', s.openCode?.url || 'http://127.0.0.1:4096', `Hidden bridge launches ${cfg.bridge?.openCodeBin || s.openCode?.detail || 'opencode'} when Hindsight needs Copilot.`)}
        ${dependencyCard('Default Memory Bank', s.hindsight?.healthy ? 'READY' : 'API OFF', 'bank: default', 'Created automatically after Hindsight API starts. Run manually if first-run setup was skipped.', `<button data-action="ensure-default-bank"${s.hindsight?.healthy ? '' : ' disabled title="Start Hindsight API first"'}>ENSURE BANK</button>`)}
        ${integrationCard('OpenCode Plugin', s.openCodePlugin, '<button data-action="install-opencode-plugin">INSTALL PLUGIN</button>')}
        ${integrationCard('OpenCode MCP', s.openCodeMcp, '<button data-action="install-opencode-mcp">INSTALL MCP</button>')}
        ${integrationCard('Codex Hooks', s.codexHooks, '<button data-action="install-codex">INSTALL HOOKS</button>')}
      </div>
      <div class="actions"><button data-action="refresh">REFRESH</button></div>
    </section>

    <section class="panel metrics-panel">
      <div class="metric"><span>MODEL</span><strong>${escapeHtml(cfg.bridge?.defaultModel || 'unset')}</strong></div>
      <div class="metric"><span>OPENCODE COMMAND</span><strong>${escapeHtml(cfg.bridge?.openCodeBin || s.openCode?.detail || 'opencode')}</strong></div>
      <div class="metric"><span>CONFIG</span><strong>${escapeHtml(s.paths?.config || '')}</strong></div>
      <div class="metric"><span>INSTALL</span><strong>${escapeHtml(s.paths?.install || '')}</strong></div>
    </section>

    <section class="panel config-panel">
      <div class="panel-head"><p class="overline">SETUP</p><h2>OpenCode Model</h2></div>
      <div class="form-grid compact">
        ${input('defaultModel', 'OPENCODE_MODEL', cfg.bridge?.defaultModel || '')}
      </div>
      <div class="actions">
        <button class="primary" data-action="save-config">SAVE SETTINGS</button>
      </div>
    </section>`;
}

function serviceCard(name, service = {}, actions = '', lifecycle = null) {
  const status = lifecycle || (service.healthy ? 'HEALTHY' : service.running ? 'STARTING' : 'STOPPED');
  const tone = status === 'HEALTHY' ? 'ok' : (status === 'STARTING' || status === 'SHUTTING DOWN') ? 'warn' : 'bad';
  return `<div class="service-card ${tone}">
    <div><strong>${escapeHtml(name)}</strong><span>${status}</span></div>
    <code>${escapeHtml(service.url || '')}</code>
    <p>${escapeHtml(service.detail || '')}</p>
    ${actions ? `<div class="card-actions">${actions}</div>` : ''}
  </div>`;
}

function integrationCard(name, item = {}, actions = '') {
  return `<div class="service-card ${item.installed ? 'ok' : 'warn'}">
    <div><strong>${escapeHtml(name)}</strong><span>${item.installed ? 'INSTALLED' : 'MISSING'}</span></div>
    <code>${escapeHtml(item.path || '')}</code>
    <p>${escapeHtml(item.detail || '')}</p>
    ${actions ? `<div class="card-actions">${actions}</div>` : ''}
  </div>`;
}

function dependencyCard(name, status, url, detail, actions = '') {
  const ok = status === 'SESSION READY' || status === 'READY';
  return `<div class="service-card ${ok ? 'ok' : 'warn'}">
    <div><strong>${escapeHtml(name)}</strong><span>${escapeHtml(status)}</span></div>
    <code>${escapeHtml(url || '')}</code>
    <p>${escapeHtml(detail || '')}</p>
    ${actions ? `<div class="card-actions">${actions}</div>` : ''}
  </div>`;
}

function openCodeConfigDialog() {
  const install = state.openCodeInstall;
  if (!install) return '';
  const label = install.kind === 'plugin' ? 'OpenCode Plugin' : 'OpenCode MCP';
  return `<div class="modal-backdrop">
    <section class="modal panel">
      <div class="panel-head"><p class="overline">OPENCODE_CONFIG</p><h2>Choose Config File</h2></div>
      <p>Both config formats may be present. Choose which file to update for ${escapeHtml(label)}.</p>
      <div class="choice-list">
        ${install.choices.map((choice) => `<button data-action="select-opencode-config" data-install-kind="${escapeAttr(install.kind)}" data-config-path="${escapeAttr(choice.path)}"><strong>${escapeHtml(choice.label)}</strong><span>${escapeHtml(choice.path)}</span></button>`).join('')}
      </div>
      <div class="actions"><button data-action="cancel-opencode-config">CANCEL</button></div>
    </section>
  </div>`;
}

function pill(label, ok) {
  return `<div class="pill ${ok ? 'online' : 'offline'}"><i></i>${label}: ${ok ? 'OK' : 'OFF'}</div>`;
}

function input(id, label, value) {
  return `<label class="field"><span>${label}</span><input id="${id}" value="${escapeHtml(value)}" /></label>`;
}

function passwordInput(id, label, value) {
  return `<label class="field"><span>${label}</span><input id="${id}" type="password" autocomplete="off" value="${escapeHtml(value)}" /></label>`;
}

function checkbox(id, label, checked) {
  return `<label class="check"><input id="${id}" type="checkbox" ${checked ? 'checked' : ''}/><span>${label}</span></label>`;
}

function serviceLifecycle(id) {
  if (state.stopping.has(id)) return 'SHUTTING DOWN';
  if (state.starting.has(id)) return 'STARTING';
  return null;
}

function bind() {
  document.querySelectorAll('[data-page]').forEach((button) => {
    button.addEventListener('click', () => {
      state.page = button.dataset.page;
      gridScrollTop = 0;
      render();
    });
  });
  document.querySelectorAll('[data-action]').forEach((button) => {
    button.addEventListener('click', async () => runAction(button.dataset.action, button));
  });
}

async function runAction(action, source) {
  try {
    markStarting(action);
    markStopping(action);
    state.message = `${action.toUpperCase()}...`;
    render();
    if (action === 'start-all') await StartAll();
    if (action === 'stop-all') await StopAll();
    if (action === 'start-hindsight') await StartHindsight();
    if (action === 'stop-hindsight') await StopHindsight();
    if (action === 'start-ui') await StartControlPlane();
    if (action === 'stop-ui') await StopControlPlane();
    if (action === 'open-ui') await OpenControlPlane();
    if (action === 'ensure-default-bank') await EnsureDefaultMemoryBank();
    if (action === 'copy-log') await CopyText((state.status?.logTail || []).join('\n'));
    if (action === 'hide-to-tray') await HideToTray();
    if (action === 'quit-app') await QuitApp();
    if (action === 'install-opencode-plugin') await beginOpenCodeInstall('plugin');
    if (action === 'install-opencode-mcp') await beginOpenCodeInstall('mcp');
    if (action === 'select-opencode-config') await finishOpenCodeInstall(source?.dataset.installKind, source?.dataset.configPath);
    if (action === 'cancel-opencode-config') state.openCodeInstall = null;
    if (action === 'install-codex') await InstallCodexHooks();
    if (action === 'save-update-settings') await saveUpdateSettings();
    if (action === 'clear-update-token') await ClearUpdateToken();
    if (action === 'check-update') await CheckForUpdate();
    if (action === 'download-update') await DownloadUpdate();
    if (action === 'install-update') await InstallDownloadedUpdate();
    if (action === 'open-release') await openExternal(state.status?.update?.releaseUrl);
    if (action === 'save-config') await saveConfig();
    if (isStopAction(action)) await delay(450);
    if (state.openCodeInstall && (action === 'install-opencode-plugin' || action === 'install-opencode-mcp')) {
      state.message = 'CHOOSE OPENCODE CONFIG';
    } else {
      state.message = `${action.toUpperCase()} OK`;
    }
  } catch (error) {
    state.message = errorMessage(error);
  }
  clearStarting(action);
  clearStopping(action);
  await refresh();
}

async function saveUpdateSettings() {
  const repo = document.querySelector('#updateRepo')?.value || '';
  const token = document.querySelector('#updateToken')?.value || '';
  const checkOnLaunch = Boolean(document.querySelector('#updateCheckOnLaunch')?.checked);
  await SaveUpdateSettings(repo, token, checkOnLaunch);
}

async function openExternal(url) {
  if (!url) return;
  window.open(url, '_blank', 'noopener,noreferrer');
}

async function beginOpenCodeInstall(kind) {
  const choices = await ListOpenCodeConfigChoices();
  if (choices.length <= 1) {
    await finishOpenCodeInstall(kind, choices[0]?.path || '');
    return;
  }
  state.openCodeInstall = { kind, choices };
  state.message = 'CHOOSE OPENCODE CONFIG';
}

async function finishOpenCodeInstall(kind, path) {
  if (!path) throw new Error('OpenCode config path is missing');
  if (kind === 'plugin') await InstallOpenCodePluginAt(path);
  if (kind === 'mcp') await InstallOpenCodeMCPAt(path);
  state.openCodeInstall = null;
}

function markStarting(action) {
  if (action === 'start-all') state.starting = new Set(['api', 'ui']);
  if (action === 'start-hindsight') state.starting = new Set([...state.starting, 'api']);
  if (action === 'start-ui') state.starting = new Set([...state.starting, 'ui']);
}

function clearStarting(action) {
  if (action === 'start-all') state.starting = new Set();
  if (action === 'start-hindsight' || action === 'start-ui') {
    const next = new Set(state.starting);
    if (action === 'start-hindsight') next.delete('api');
    if (action === 'start-ui') next.delete('ui');
    state.starting = next;
  }
}

function markStopping(action) {
  if (action === 'stop-all') state.stopping = new Set(['api', 'ui']);
  if (action === 'stop-hindsight') state.stopping = new Set([...state.stopping, 'api', 'ui']);
  if (action === 'stop-ui') state.stopping = new Set([...state.stopping, 'ui']);
}

function clearStopping(action) {
  if (action === 'stop-all') state.stopping = new Set();
  if (action === 'stop-hindsight' || action === 'stop-ui') {
    const next = new Set(state.stopping);
    if (action === 'stop-hindsight') {
      next.delete('api');
      next.delete('ui');
    }
    if (action === 'stop-ui') next.delete('ui');
    state.stopping = next;
  }
}

function isStopAction(action) {
  return action === 'stop-all' || action === 'stop-hindsight' || action === 'stop-ui';
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function saveConfig() {
  const cfg = state.config || await LoadConfig();
  cfg.bridge = cfg.bridge || {};
  const startServices = document.querySelector('#startServicesOnLaunch');
  const startUi = document.querySelector('#startUiOnLaunch');
  const openBrowser = document.querySelector('#openUiBrowserOnLaunch');
  const closeToTray = document.querySelector('#closeToTray');
  const hindsightPort = document.querySelector('#hindsightPort');
  const controlPlanePort = document.querySelector('#controlPlanePort');
  const defaultModel = document.querySelector('#defaultModel');
  if (startServices) cfg.startServicesOnLaunch = startServices.checked;
  if (startUi) cfg.startUiOnLaunch = startUi.checked;
  if (openBrowser) cfg.openUiBrowserOnLaunch = openBrowser.checked;
  if (closeToTray) cfg.bridge.closeToTray = closeToTray.checked;
  if (hindsightPort) cfg.hindsightPort = hindsightPort.value || '8888';
  if (controlPlanePort) cfg.controlPlanePort = controlPlanePort.value || '9999';
  if (defaultModel) cfg.bridge.defaultModel = defaultModel.value || cfg.bridge.defaultModel;
  await SaveConfig(cfg);
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>'"]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#039;', '"': '&quot;' }[char]));
}

function escapeAttr(value) {
  return escapeHtml(value).replace(/`/g, '&#096;');
}

function errorMessage(error) {
  return typeof error === 'string' ? error : (error?.message || JSON.stringify(error));
}

refresh();
setInterval(() => {
  if (!document.querySelector('input:focus')) refresh();
}, 5000);
