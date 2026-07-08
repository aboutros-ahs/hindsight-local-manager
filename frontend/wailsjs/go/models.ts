export namespace main {
	
	export class BridgeConfig {
	    host: string;
	    port: string;
	    projectDir: string;
	    defaultModel: string;
	    openCodeBin: string;
	    sessionMode: string;
	    logDir: string;
	    logBodies: boolean;
	    closeToTray: boolean;
	    minimizeToTray: boolean;
	    modelAliases: string;
	    openCodeServerUrl: string;
	    openCodeAgent: string;
	    openCodeTools: string;
	
	    static createFrom(source: any = {}) {
	        return new BridgeConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.projectDir = source["projectDir"];
	        this.defaultModel = source["defaultModel"];
	        this.openCodeBin = source["openCodeBin"];
	        this.sessionMode = source["sessionMode"];
	        this.logDir = source["logDir"];
	        this.logBodies = source["logBodies"];
	        this.closeToTray = source["closeToTray"];
	        this.minimizeToTray = source["minimizeToTray"];
	        this.modelAliases = source["modelAliases"];
	        this.openCodeServerUrl = source["openCodeServerUrl"];
	        this.openCodeAgent = source["openCodeAgent"];
	        this.openCodeTools = source["openCodeTools"];
	    }
	}
	export class IntegrationStatus {
	    installed: boolean;
	    path: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new IntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.path = source["path"];
	        this.detail = source["detail"];
	    }
	}
	export class UpdateConfig {
	    githubRepo: string;
	    checkOnLaunch: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.githubRepo = source["githubRepo"];
	        this.checkOnLaunch = source["checkOnLaunch"];
	    }
	}
	export class ManagerConfig {
	    bridge: BridgeConfig;
	    update: UpdateConfig;
	    startServicesOnLaunch: boolean;
	    startUiOnLaunch: boolean;
	    openUiBrowserOnLaunch: boolean;
	    hindsightHost: string;
	    hindsightPort: string;
	    controlPlanePort: string;
	    dynamicBankIds: boolean;
	    autostart: boolean;
	    debug: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ManagerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bridge = this.convertValues(source["bridge"], BridgeConfig);
	        this.update = this.convertValues(source["update"], UpdateConfig);
	        this.startServicesOnLaunch = source["startServicesOnLaunch"];
	        this.startUiOnLaunch = source["startUiOnLaunch"];
	        this.openUiBrowserOnLaunch = source["openUiBrowserOnLaunch"];
	        this.hindsightHost = source["hindsightHost"];
	        this.hindsightPort = source["hindsightPort"];
	        this.controlPlanePort = source["controlPlanePort"];
	        this.dynamicBankIds = source["dynamicBankIds"];
	        this.autostart = source["autostart"];
	        this.debug = source["debug"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpdateStatus {
	    currentVersion: string;
	    latestVersion: string;
	    hasUpdate: boolean;
	    state: string;
	    message: string;
	    progress: number;
	    assetName: string;
	    releaseUrl: string;
	    downloadPath: string;
	    tokenConfigured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.hasUpdate = source["hasUpdate"];
	        this.state = source["state"];
	        this.message = source["message"];
	        this.progress = source["progress"];
	        this.assetName = source["assetName"];
	        this.releaseUrl = source["releaseUrl"];
	        this.downloadPath = source["downloadPath"];
	        this.tokenConfigured = source["tokenConfigured"];
	    }
	}
	export class ServiceStatus {
	    running: boolean;
	    healthy: boolean;
	    url: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.healthy = source["healthy"];
	        this.url = source["url"];
	        this.detail = source["detail"];
	    }
	}
	export class ManagerStatus {
	    config: ManagerConfig;
	    openCode: ServiceStatus;
	    bridge: ServiceStatus;
	    hindsight: ServiceStatus;
	    mcp: ServiceStatus;
	    controlPlane: ServiceStatus;
	    openCodePlugin: IntegrationStatus;
	    openCodeMcp: IntegrationStatus;
	    codexHooks: IntegrationStatus;
	    apiKey: string;
	    models: string[];
	    logTail: string[];
	    paths: Record<string, string>;
	    version: string;
	    update: UpdateStatus;
	    lastUpdated: string;
	
	    static createFrom(source: any = {}) {
	        return new ManagerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], ManagerConfig);
	        this.openCode = this.convertValues(source["openCode"], ServiceStatus);
	        this.bridge = this.convertValues(source["bridge"], ServiceStatus);
	        this.hindsight = this.convertValues(source["hindsight"], ServiceStatus);
	        this.mcp = this.convertValues(source["mcp"], ServiceStatus);
	        this.controlPlane = this.convertValues(source["controlPlane"], ServiceStatus);
	        this.openCodePlugin = this.convertValues(source["openCodePlugin"], IntegrationStatus);
	        this.openCodeMcp = this.convertValues(source["openCodeMcp"], IntegrationStatus);
	        this.codexHooks = this.convertValues(source["codexHooks"], IntegrationStatus);
	        this.apiKey = source["apiKey"];
	        this.models = source["models"];
	        this.logTail = source["logTail"];
	        this.paths = source["paths"];
	        this.version = source["version"];
	        this.update = this.convertValues(source["update"], UpdateStatus);
	        this.lastUpdated = source["lastUpdated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenCodeConfigChoice {
	    label: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeConfigChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.path = source["path"];
	    }
	}
	
	export class TrayManager {
	
	
	    static createFrom(source: any = {}) {
	        return new TrayManager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	

}

