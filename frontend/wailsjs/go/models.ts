export namespace hottrend {
	
	export class HotItem {
	    id: string;
	    title: string;
	    url: string;
	    hot_score: number;
	    rank: number;
	    platform: string;
	    extra: string;
	
	    static createFrom(source: any = {}) {
	        return new HotItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.hot_score = source["hot_score"];
	        this.rank = source["rank"];
	        this.platform = source["platform"];
	        this.extra = source["extra"];
	    }
	}
	export class HotTrendResult {
	    platform: string;
	    platform_cn: string;
	    items: HotItem[];
	    // Go type: time
	    updated_at: any;
	    from_cache: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new HotTrendResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.platform_cn = source["platform_cn"];
	        this.items = this.convertValues(source["items"], HotItem);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.from_cache = source["from_cache"];
	        this.error = source["error"];
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
	export class PlatformInfo {
	    ID: string;
	    Name: string;
	    HomeURL: string;
	
	    static createFrom(source: any = {}) {
	        return new PlatformInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.HomeURL = source["HomeURL"];
	    }
	}

}

export namespace main {
	
	export class App {
	
	
	    static createFrom(source: any = {}) {
	        return new App(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class AskBoardReportRequest {
	    stockCode: string;
	    report: string;
	    question: string;
	
	    static createFrom(source: any = {}) {
	        return new AskBoardReportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.report = source["report"];
	        this.question = source["question"];
	    }
	}
	export class AskBoardReportResponse {
	    success: boolean;
	    stockCode?: string;
	    answer?: string;
	    modelName?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AskBoardReportResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.stockCode = source["stockCode"];
	        this.answer = source["answer"];
	        this.modelName = source["modelName"];
	        this.error = source["error"];
	    }
	}
	export class AuditEntry {
	    id: number;
	    time: string;
	    username: string;
	    method: string;
	    args: string;
	    ip: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.time = source["time"];
	        this.username = source["username"];
	        this.method = source["method"];
	        this.args = source["args"];
	        this.ip = source["ip"];
	    }
	}
	export class AuditUserSummary {
	    username: string;
	    count: number;
	    lastTime: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditUserSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.count = source["count"];
	        this.lastTime = source["lastTime"];
	    }
	}
	export class BackendMode {
	    mode: string;
	    url: string;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new BackendMode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.url = source["url"];
	        this.token = source["token"];
	    }
	}
	export class BoardReportStatus {
	    status: string;
	    elapsedSec?: number;
	    stockCode?: string;
	    stockName?: string;
	    report?: string;
	    agentId?: string;
	    agentName?: string;
	    modelName?: string;
	    generatedAt?: string;
	    fromCache?: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardReportStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.elapsedSec = source["elapsedSec"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.report = source["report"];
	        this.agentId = source["agentId"];
	        this.agentName = source["agentName"];
	        this.modelName = source["modelName"];
	        this.generatedAt = source["generatedAt"];
	        this.fromCache = source["fromCache"];
	        this.error = source["error"];
	    }
	}
	export class EnhancePromptRequest {
	    originalPrompt: string;
	    agentRole: string;
	    agentName: string;
	
	    static createFrom(source: any = {}) {
	        return new EnhancePromptRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.originalPrompt = source["originalPrompt"];
	        this.agentRole = source["agentRole"];
	        this.agentName = source["agentName"];
	    }
	}
	export class EnhancePromptResponse {
	    success: boolean;
	    enhancedPrompt?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new EnhancePromptResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.enhancedPrompt = source["enhancedPrompt"];
	        this.error = source["error"];
	    }
	}
	export class GenerateBoardReportRequest {
	    stockCode: string;
	    stockName: string;
	    period?: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateBoardReportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.period = source["period"];
	    }
	}
	export class GenerateBoardReportResponse {
	    success: boolean;
	    stockCode?: string;
	    stockName?: string;
	    report?: string;
	    agentId?: string;
	    agentName?: string;
	    modelName?: string;
	    generatedAt?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateBoardReportResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.report = source["report"];
	        this.agentId = source["agentId"];
	        this.agentName = source["agentName"];
	        this.modelName = source["modelName"];
	        this.generatedAt = source["generatedAt"];
	        this.error = source["error"];
	    }
	}
	export class GenerateStrategyRequest {
	    prompt: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateStrategyRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt = source["prompt"];
	    }
	}
	export class GenerateStrategyResponse {
	    success: boolean;
	    error?: string;
	    strategy?: models.Strategy;
	    reasoning?: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateStrategyResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	        this.strategy = this.convertValues(source["strategy"], models.Strategy);
	        this.reasoning = source["reasoning"];
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
	export class GetCachedBoardReportResponse {
	    success: boolean;
	    found: boolean;
	    stockCode?: string;
	    stockName?: string;
	    report?: string;
	    agentId?: string;
	    agentName?: string;
	    modelName?: string;
	    generatedAt?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GetCachedBoardReportResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.found = source["found"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.report = source["report"];
	        this.agentId = source["agentId"];
	        this.agentName = source["agentName"];
	        this.modelName = source["modelName"];
	        this.generatedAt = source["generatedAt"];
	        this.error = source["error"];
	    }
	}
	export class IntelDigestResponse {
	    success: boolean;
	    digest: string;
	    noteCount: number;
	    holdCount: number;
	    generatedAt: string;
	    modelName?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new IntelDigestResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.digest = source["digest"];
	        this.noteCount = source["noteCount"];
	        this.holdCount = source["holdCount"];
	        this.generatedAt = source["generatedAt"];
	        this.modelName = source["modelName"];
	        this.error = source["error"];
	    }
	}
	export class IntelNote {
	    id: number;
	    createdAt: string;
	    text: string;
	    source: string;
	    codes: string[];
	
	    static createFrom(source: any = {}) {
	        return new IntelNote(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.text = source["text"];
	        this.source = source["source"];
	        this.codes = source["codes"];
	    }
	}
	export class LoginResponse {
	    success: boolean;
	    token: string;
	    username: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.token = source["token"];
	        this.username = source["username"];
	        this.error = source["error"];
	    }
	}
	export class MeetingMessageRequest {
	    stockCode: string;
	    content: string;
	    mentionIds: string[];
	    replyToId: string;
	    replyContent: string;
	    battle: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MeetingMessageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.content = source["content"];
	        this.mentionIds = source["mentionIds"];
	        this.replyToId = source["replyToId"];
	        this.replyContent = source["replyContent"];
	        this.battle = source["battle"];
	    }
	}
	export class ResearchReportStatus {
	    status: string;
	    stockCode: string;
	    stockName: string;
	    report: string;
	    fileName: string;
	    filePath: string;
	    error: string;
	    modelName: string;
	    startedAt: string;
	    finishedAt: string;
	    elapsedSec: number;
	
	    static createFrom(source: any = {}) {
	        return new ResearchReportStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.report = source["report"];
	        this.fileName = source["fileName"];
	        this.filePath = source["filePath"];
	        this.error = source["error"];
	        this.modelName = source["modelName"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.elapsedSec = source["elapsedSec"];
	    }
	}
	export class StockIntradayResult {
	    code: string;
	    date: string;
	    auction: services.IntradayTick[];
	    minutes: services.IntradayTick[];
	
	    static createFrom(source: any = {}) {
	        return new StockIntradayResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.date = source["date"];
	        this.auction = this.convertValues(source["auction"], services.IntradayTick);
	        this.minutes = this.convertValues(source["minutes"], services.IntradayTick);
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

}

export namespace mcp {
	
	export class ServerStatus {
	    id: string;
	    connected: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connected = source["connected"];
	        this.error = source["error"];
	    }
	}
	export class ToolInfo {
	    name: string;
	    description: string;
	    serverId: string;
	    serverName: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	    }
	}

}

export namespace models {
	
	export class AIConfig {
	    id: string;
	    name: string;
	    provider: string;
	    baseUrl: string;
	    apiKey: string;
	    modelName: string;
	    maxTokens: number;
	    tokenParamMode: string;
	    temperature: number;
	    timeout: number;
	    isDefault: boolean;
	    useResponses: boolean;
	    noSystemRole: boolean;
	    project: string;
	    location: string;
	    credentialsJson: string;
	
	    static createFrom(source: any = {}) {
	        return new AIConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.modelName = source["modelName"];
	        this.maxTokens = source["maxTokens"];
	        this.tokenParamMode = source["tokenParamMode"];
	        this.temperature = source["temperature"];
	        this.timeout = source["timeout"];
	        this.isDefault = source["isDefault"];
	        this.useResponses = source["useResponses"];
	        this.noSystemRole = source["noSystemRole"];
	        this.project = source["project"];
	        this.location = source["location"];
	        this.credentialsJson = source["credentialsJson"];
	    }
	}
	export class AccountEquityPoint {
	    date: string;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountEquityPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.value = source["value"];
	    }
	}
	export class AccountHolding {
	    symbol: string;
	    name: string;
	    entryDate: string;
	    entryPrice: number;
	    currentPrice: number;
	    holdDays: number;
	    unrealizedPct: number;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountHolding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.entryDate = source["entryDate"];
	        this.entryPrice = source["entryPrice"];
	        this.currentPrice = source["currentPrice"];
	        this.holdDays = source["holdDays"];
	        this.unrealizedPct = source["unrealizedPct"];
	        this.value = source["value"];
	    }
	}
	export class AccountTrade {
	    symbol: string;
	    name: string;
	    entryDate: string;
	    exitDate: string;
	    entryPrice: number;
	    exitPrice: number;
	    holdDays: number;
	    returnPct: number;
	    exitReason: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountTrade(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.entryDate = source["entryDate"];
	        this.exitDate = source["exitDate"];
	        this.entryPrice = source["entryPrice"];
	        this.exitPrice = source["exitPrice"];
	        this.holdDays = source["holdDays"];
	        this.returnPct = source["returnPct"];
	        this.exitReason = source["exitReason"];
	    }
	}
	export class AgentConfig {
	    id: string;
	    name: string;
	    role: string;
	    avatar: string;
	    color: string;
	    instruction: string;
	    tools: string[];
	    mcpServers: string[];
	    enabled: boolean;
	    aiConfigId: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.role = source["role"];
	        this.avatar = source["avatar"];
	        this.color = source["color"];
	        this.instruction = source["instruction"];
	        this.tools = source["tools"];
	        this.mcpServers = source["mcpServers"];
	        this.enabled = source["enabled"];
	        this.aiConfigId = source["aiConfigId"];
	    }
	}
	export class RemoteUser {
	    username: string;
	    passwordHash: string;
	    trusted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.passwordHash = source["passwordHash"];
	        this.trusted = source["trusted"];
	    }
	}
	export class TailForwardConfig {
	    enabled: boolean;
	    auto: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TailForwardConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.auto = source["auto"];
	    }
	}
	export class MonitorConfig {
	    enabled: boolean;
	    intervalMinutes: number;
	    afterMarketCheck: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MonitorConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.intervalMinutes = source["intervalMinutes"];
	        this.afterMarketCheck = source["afterMarketCheck"];
	    }
	}
	export class WebhookChannel {
	    enabled: boolean;
	    webhook: string;
	
	    static createFrom(source: any = {}) {
	        return new WebhookChannel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.webhook = source["webhook"];
	    }
	}
	export class TelegramChannel {
	    enabled: boolean;
	    botToken: string;
	    chatId: string;
	
	    static createFrom(source: any = {}) {
	        return new TelegramChannel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.botToken = source["botToken"];
	        this.chatId = source["chatId"];
	    }
	}
	export class BarkChannel {
	    enabled: boolean;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new BarkChannel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.url = source["url"];
	    }
	}
	export class PushConfig {
	    enabled: boolean;
	    dedupHours: number;
	    pushProxyUrl: string;
	    bark: BarkChannel;
	    telegram: TelegramChannel;
	    feishu: WebhookChannel;
	    weWork: WebhookChannel;
	    monitor: MonitorConfig;
	
	    static createFrom(source: any = {}) {
	        return new PushConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.dedupHours = source["dedupHours"];
	        this.pushProxyUrl = source["pushProxyUrl"];
	        this.bark = this.convertValues(source["bark"], BarkChannel);
	        this.telegram = this.convertValues(source["telegram"], TelegramChannel);
	        this.feishu = this.convertValues(source["feishu"], WebhookChannel);
	        this.weWork = this.convertValues(source["weWork"], WebhookChannel);
	        this.monitor = this.convertValues(source["monitor"], MonitorConfig);
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
	export class HistoryConfig {
	    autoCollectDaily: boolean;
	    collectStart: string;
	    collectEnd: string;
	    includeBeijing: boolean;
	    lastCollectDate: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoCollectDaily = source["autoCollectDaily"];
	        this.collectStart = source["collectStart"];
	        this.collectEnd = source["collectEnd"];
	        this.includeBeijing = source["includeBeijing"];
	        this.lastCollectDate = source["lastCollectDate"];
	    }
	}
	export class KDJConfig {
	    enabled: boolean;
	    period: number;
	    k: number;
	    d: number;
	
	    static createFrom(source: any = {}) {
	        return new KDJConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.period = source["period"];
	        this.k = source["k"];
	        this.d = source["d"];
	    }
	}
	export class RSIConfig {
	    enabled: boolean;
	    period: number;
	
	    static createFrom(source: any = {}) {
	        return new RSIConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.period = source["period"];
	    }
	}
	export class MACDConfig {
	    enabled: boolean;
	    fast: number;
	    slow: number;
	    signal: number;
	
	    static createFrom(source: any = {}) {
	        return new MACDConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.fast = source["fast"];
	        this.slow = source["slow"];
	        this.signal = source["signal"];
	    }
	}
	export class BOLLConfig {
	    enabled: boolean;
	    period: number;
	    multiplier: number;
	
	    static createFrom(source: any = {}) {
	        return new BOLLConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.period = source["period"];
	        this.multiplier = source["multiplier"];
	    }
	}
	export class EMAConfig {
	    enabled: boolean;
	    periods: number[];
	
	    static createFrom(source: any = {}) {
	        return new EMAConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.periods = source["periods"];
	    }
	}
	export class MAConfig {
	    enabled: boolean;
	    periods: number[];
	
	    static createFrom(source: any = {}) {
	        return new MAConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.periods = source["periods"];
	    }
	}
	export class IndicatorConfig {
	    ma: MAConfig;
	    ema: EMAConfig;
	    boll: BOLLConfig;
	    macd: MACDConfig;
	    rsi: RSIConfig;
	    kdj: KDJConfig;
	
	    static createFrom(source: any = {}) {
	        return new IndicatorConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ma = this.convertValues(source["ma"], MAConfig);
	        this.ema = this.convertValues(source["ema"], EMAConfig);
	        this.boll = this.convertValues(source["boll"], BOLLConfig);
	        this.macd = this.convertValues(source["macd"], MACDConfig);
	        this.rsi = this.convertValues(source["rsi"], RSIConfig);
	        this.kdj = this.convertValues(source["kdj"], KDJConfig);
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
	export class OpenClawConfig {
	    enabled: boolean;
	    port: number;
	    apiKey: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenClawConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.apiKey = source["apiKey"];
	    }
	}
	export class LayoutConfig {
	    leftPanelWidth: number;
	    rightPanelWidth: number;
	    bottomPanelHeight: number;
	    windowWidth: number;
	    windowHeight: number;
	
	    static createFrom(source: any = {}) {
	        return new LayoutConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leftPanelWidth = source["leftPanelWidth"];
	        this.rightPanelWidth = source["rightPanelWidth"];
	        this.bottomPanelHeight = source["bottomPanelHeight"];
	        this.windowWidth = source["windowWidth"];
	        this.windowHeight = source["windowHeight"];
	    }
	}
	export class ProxyConfig {
	    mode: string;
	    customUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.customUrl = source["customUrl"];
	    }
	}
	export class MemoryConfig {
	    enabled: boolean;
	    aiConfigId: string;
	    maxRecentRounds: number;
	    maxKeyFacts: number;
	    maxSummaryLength: number;
	    compressThreshold: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.aiConfigId = source["aiConfigId"];
	        this.maxRecentRounds = source["maxRecentRounds"];
	        this.maxKeyFacts = source["maxKeyFacts"];
	        this.maxSummaryLength = source["maxSummaryLength"];
	        this.compressThreshold = source["compressThreshold"];
	    }
	}
	export class MCPServerConfig {
	    id: string;
	    name: string;
	    transportType: string;
	    endpoint: string;
	    command: string;
	    args: string[];
	    toolFilter: string[];
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.transportType = source["transportType"];
	        this.endpoint = source["endpoint"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.toolFilter = source["toolFilter"];
	        this.enabled = source["enabled"];
	    }
	}
	export class AppConfig {
	    theme: string;
	    candleColorMode: string;
	    aiConfigs: AIConfig[];
	    defaultAiId: string;
	    strategyAiId: string;
	    moderatorAiId: string;
	    aiRetryCount: number;
	    verboseAgentIO: boolean;
	    agentSelectionStyle: string;
	    enableSecondReview: boolean;
	    mcpServers: MCPServerConfig[];
	    memory: MemoryConfig;
	    proxy: ProxyConfig;
	    layout: LayoutConfig;
	    openClaw: OpenClawConfig;
	    indicators: IndicatorConfig;
	    history: HistoryConfig;
	    push: PushConfig;
	    tailForward: TailForwardConfig;
	    remoteBackendUrl: string;
	    remoteBackendPublicUrl: string;
	    remoteBackendToken: string;
	    remoteUsers: RemoteUser[];
	    registerInviteCode: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.candleColorMode = source["candleColorMode"];
	        this.aiConfigs = this.convertValues(source["aiConfigs"], AIConfig);
	        this.defaultAiId = source["defaultAiId"];
	        this.strategyAiId = source["strategyAiId"];
	        this.moderatorAiId = source["moderatorAiId"];
	        this.aiRetryCount = source["aiRetryCount"];
	        this.verboseAgentIO = source["verboseAgentIO"];
	        this.agentSelectionStyle = source["agentSelectionStyle"];
	        this.enableSecondReview = source["enableSecondReview"];
	        this.mcpServers = this.convertValues(source["mcpServers"], MCPServerConfig);
	        this.memory = this.convertValues(source["memory"], MemoryConfig);
	        this.proxy = this.convertValues(source["proxy"], ProxyConfig);
	        this.layout = this.convertValues(source["layout"], LayoutConfig);
	        this.openClaw = this.convertValues(source["openClaw"], OpenClawConfig);
	        this.indicators = this.convertValues(source["indicators"], IndicatorConfig);
	        this.history = this.convertValues(source["history"], HistoryConfig);
	        this.push = this.convertValues(source["push"], PushConfig);
	        this.tailForward = this.convertValues(source["tailForward"], TailForwardConfig);
	        this.remoteBackendUrl = source["remoteBackendUrl"];
	        this.remoteBackendPublicUrl = source["remoteBackendPublicUrl"];
	        this.remoteBackendToken = source["remoteBackendToken"];
	        this.remoteUsers = this.convertValues(source["remoteUsers"], RemoteUser);
	        this.registerInviteCode = source["registerInviteCode"];
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
	
	export class BacktestRequest {
	    days: number;
	    topN: number;
	    entryRule: string;
	    maxMarketCap: number;
	    sellRule: string;
	    maxPositions: number;
	    takeProfitPct: number;
	    stopLossPct: number;
	    costPct: number;
	    gateMode: string;
	    universe: string;
	    engine: string;
	    maxChangePct?: number;
	
	    static createFrom(source: any = {}) {
	        return new BacktestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.days = source["days"];
	        this.topN = source["topN"];
	        this.entryRule = source["entryRule"];
	        this.maxMarketCap = source["maxMarketCap"];
	        this.sellRule = source["sellRule"];
	        this.maxPositions = source["maxPositions"];
	        this.takeProfitPct = source["takeProfitPct"];
	        this.stopLossPct = source["stopLossPct"];
	        this.costPct = source["costPct"];
	        this.gateMode = source["gateMode"];
	        this.universe = source["universe"];
	        this.engine = source["engine"];
	        this.maxChangePct = source["maxChangePct"];
	    }
	}
	export class BacktestTrade {
	    code: string;
	    name: string;
	    signalDate: string;
	    entryDate: string;
	    entryPrice: number;
	    exitDate: string;
	    exitPrice: number;
	    holdDays: number;
	    returnPct: number;
	    exitReason: string;
	    score: number;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new BacktestTrade(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.signalDate = source["signalDate"];
	        this.entryDate = source["entryDate"];
	        this.entryPrice = source["entryPrice"];
	        this.exitDate = source["exitDate"];
	        this.exitPrice = source["exitPrice"];
	        this.holdDays = source["holdDays"];
	        this.returnPct = source["returnPct"];
	        this.exitReason = source["exitReason"];
	        this.score = source["score"];
	        this.source = source["source"];
	    }
	}
	export class BacktestResult {
	    startDate: string;
	    endDate: string;
	    tradingDays: number;
	    totalTrades: number;
	    winTrades: number;
	    winRate: number;
	    avgReturn: number;
	    avgWin: number;
	    avgLoss: number;
	    profitFactor: number;
	    payoffRatio: number;
	    maxLossPct: number;
	    benchmarkPct: number;
	    excessPct: number;
	    maxDrawdown: number;
	    avgHoldDays: number;
	    totalReturn: number;
	    byReason: Record<string, number>;
	    trades: BacktestTrade[];
	    status: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new BacktestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.tradingDays = source["tradingDays"];
	        this.totalTrades = source["totalTrades"];
	        this.winTrades = source["winTrades"];
	        this.winRate = source["winRate"];
	        this.avgReturn = source["avgReturn"];
	        this.avgWin = source["avgWin"];
	        this.avgLoss = source["avgLoss"];
	        this.profitFactor = source["profitFactor"];
	        this.payoffRatio = source["payoffRatio"];
	        this.maxLossPct = source["maxLossPct"];
	        this.benchmarkPct = source["benchmarkPct"];
	        this.excessPct = source["excessPct"];
	        this.maxDrawdown = source["maxDrawdown"];
	        this.avgHoldDays = source["avgHoldDays"];
	        this.totalReturn = source["totalReturn"];
	        this.byReason = source["byReason"];
	        this.trades = this.convertValues(source["trades"], BacktestTrade);
	        this.status = source["status"];
	        this.message = source["message"];
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
	
	
	export class BoardFundFlowItem {
	    code: string;
	    name: string;
	    price: number;
	    changePercent: number;
	    mainNetInflow: number;
	    mainNetInflowRatio: number;
	    superNetInflow: number;
	    superNetInflowRatio: number;
	    largeNetInflow: number;
	    largeNetInflowRatio: number;
	    mediumNetInflow: number;
	    mediumNetInflowRatio: number;
	    smallNetInflow: number;
	    smallNetInflowRatio: number;
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardFundFlowItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.changePercent = source["changePercent"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.mainNetInflowRatio = source["mainNetInflowRatio"];
	        this.superNetInflow = source["superNetInflow"];
	        this.superNetInflowRatio = source["superNetInflowRatio"];
	        this.largeNetInflow = source["largeNetInflow"];
	        this.largeNetInflowRatio = source["largeNetInflowRatio"];
	        this.mediumNetInflow = source["mediumNetInflow"];
	        this.mediumNetInflowRatio = source["mediumNetInflowRatio"];
	        this.smallNetInflow = source["smallNetInflow"];
	        this.smallNetInflowRatio = source["smallNetInflowRatio"];
	        this.updateTime = source["updateTime"];
	    }
	}
	export class BoardFundFlowList {
	    category: string;
	    items: BoardFundFlowItem[];
	    total?: number;
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardFundFlowList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.items = this.convertValues(source["items"], BoardFundFlowItem);
	        this.total = source["total"];
	        this.updateTime = source["updateTime"];
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
	export class BoardFundFlowOverview {
	    category: string;
	    netMainInflow: number;
	    strongestInflow?: BoardFundFlowItem;
	    strongestOutflow?: BoardFundFlowItem;
	    inflow: BoardFundFlowItem[];
	    outflow: BoardFundFlowItem[];
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardFundFlowOverview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.netMainInflow = source["netMainInflow"];
	        this.strongestInflow = this.convertValues(source["strongestInflow"], BoardFundFlowItem);
	        this.strongestOutflow = this.convertValues(source["strongestOutflow"], BoardFundFlowItem);
	        this.inflow = this.convertValues(source["inflow"], BoardFundFlowItem);
	        this.outflow = this.convertValues(source["outflow"], BoardFundFlowItem);
	        this.updateTime = source["updateTime"];
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
	export class FundFlowKLine {
	    time: string;
	    mainNetInflow: number;
	    superNetInflow: number;
	    largeNetInflow: number;
	    mediumNetInflow: number;
	    smallNetInflow: number;
	
	    static createFrom(source: any = {}) {
	        return new FundFlowKLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.superNetInflow = source["superNetInflow"];
	        this.largeNetInflow = source["largeNetInflow"];
	        this.mediumNetInflow = source["mediumNetInflow"];
	        this.smallNetInflow = source["smallNetInflow"];
	    }
	}
	export class BoardFundFlowTrackItem {
	    rank: number;
	    code: string;
	    name: string;
	    category: string;
	    side: string;
	    changePercent: number;
	    mainNetInflow: number;
	    latestMainNetInflow: number;
	    klines: FundFlowKLine[];
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardFundFlowTrackItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rank = source["rank"];
	        this.code = source["code"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.side = source["side"];
	        this.changePercent = source["changePercent"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.latestMainNetInflow = source["latestMainNetInflow"];
	        this.klines = this.convertValues(source["klines"], FundFlowKLine);
	        this.updateTime = source["updateTime"];
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
	export class BoardFundFlowTracking {
	    category: string;
	    source?: string;
	    tradeDate?: string;
	    tradeTime?: string;
	    updateTime?: string;
	    totalAmount?: number;
	    upCount?: number;
	    downCount?: number;
	    limitUpCount?: number;
	    limitDownCount?: number;
	    inflow: BoardFundFlowTrackItem[];
	    outflow: BoardFundFlowTrackItem[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardFundFlowTracking(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.source = source["source"];
	        this.tradeDate = source["tradeDate"];
	        this.tradeTime = source["tradeTime"];
	        this.updateTime = source["updateTime"];
	        this.totalAmount = source["totalAmount"];
	        this.upCount = source["upCount"];
	        this.downCount = source["downCount"];
	        this.limitUpCount = source["limitUpCount"];
	        this.limitDownCount = source["limitDownCount"];
	        this.inflow = this.convertValues(source["inflow"], BoardFundFlowTrackItem);
	        this.outflow = this.convertValues(source["outflow"], BoardFundFlowTrackItem);
	        this.warning = source["warning"];
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
	export class BoardLeaderItem {
	    rank: number;
	    code: string;
	    name: string;
	    price: number;
	    changePercent: number;
	    turnoverRate?: number;
	    mainNetInflow: number;
	    mainNetInflowRatio: number;
	    score: number;
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardLeaderItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rank = source["rank"];
	        this.code = source["code"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.changePercent = source["changePercent"];
	        this.turnoverRate = source["turnoverRate"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.mainNetInflowRatio = source["mainNetInflowRatio"];
	        this.score = source["score"];
	        this.updateTime = source["updateTime"];
	    }
	}
	export class BoardLeaderList {
	    boardCode: string;
	    items: BoardLeaderItem[];
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new BoardLeaderList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.boardCode = source["boardCode"];
	        this.items = this.convertValues(source["items"], BoardLeaderItem);
	        this.updateTime = source["updateTime"];
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
	export class BonusFinancing {
	    dividend?: any[];
	    annual?: any[];
	    financing?: any[];
	    allotment?: any[];
	
	    static createFrom(source: any = {}) {
	        return new BonusFinancing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dividend = source["dividend"];
	        this.annual = source["annual"];
	        this.financing = source["financing"];
	        this.allotment = source["allotment"];
	    }
	}
	export class BusinessAnalysis {
	    scope?: any[];
	    composition?: any[];
	    review?: any[];
	
	    static createFrom(source: any = {}) {
	        return new BusinessAnalysis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.composition = source["composition"];
	        this.review = source["review"];
	    }
	}
	export class ChatMessage {
	    id: string;
	    agentId: string;
	    agentName: string;
	    role: string;
	    content: string;
	    timestamp: number;
	    replyTo?: string;
	    mentions?: string[];
	    round?: number;
	    msgType?: string;
	    error?: string;
	    meetingMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agentId = source["agentId"];
	        this.agentName = source["agentName"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.timestamp = source["timestamp"];
	        this.replyTo = source["replyTo"];
	        this.mentions = source["mentions"];
	        this.round = source["round"];
	        this.msgType = source["msgType"];
	        this.error = source["error"];
	        this.meetingMode = source["meetingMode"];
	    }
	}
	export class CompositeScoreRow {
	    symbol: string;
	    name: string;
	    price: number;
	    quality: number;
	    structure: number;
	    catalyst: number;
	    total: number;
	    qualityFacts: string[];
	    structFacts: string[];
	    catalystFacts: string[];
	    gateOk: boolean;
	    gateReasons: string[];
	    vetoed: boolean;
	    vetoReasons: string[];
	    annRoe: number;
	    marketCapYi: number;
	
	    static createFrom(source: any = {}) {
	        return new CompositeScoreRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.quality = source["quality"];
	        this.structure = source["structure"];
	        this.catalyst = source["catalyst"];
	        this.total = source["total"];
	        this.qualityFacts = source["qualityFacts"];
	        this.structFacts = source["structFacts"];
	        this.catalystFacts = source["catalystFacts"];
	        this.gateOk = source["gateOk"];
	        this.gateReasons = source["gateReasons"];
	        this.vetoed = source["vetoed"];
	        this.vetoReasons = source["vetoReasons"];
	        this.annRoe = source["annRoe"];
	        this.marketCapYi = source["marketCapYi"];
	    }
	}
	export class CompositeScoreResult {
	    runDate: string;
	    preset: string;
	    presetLabel: string;
	    universeCount: number;
	    rows: CompositeScoreRow[];
	    vetoedRows: CompositeScoreRow[];
	    warning: string;
	    rulesText: string;
	    snapshotSaved: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CompositeScoreResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runDate = source["runDate"];
	        this.preset = source["preset"];
	        this.presetLabel = source["presetLabel"];
	        this.universeCount = source["universeCount"];
	        this.rows = this.convertValues(source["rows"], CompositeScoreRow);
	        this.vetoedRows = this.convertValues(source["vetoedRows"], CompositeScoreRow);
	        this.warning = source["warning"];
	        this.rulesText = source["rulesText"];
	        this.snapshotSaved = source["snapshotSaved"];
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
	
	export class CompositeValidationRow {
	    runDate: string;
	    horizonDays: number;
	    n: number;
	    portRet: number;
	    benchRet: number;
	    excess: number;
	    matured: boolean;
	    daysElapsed: number;
	
	    static createFrom(source: any = {}) {
	        return new CompositeValidationRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runDate = source["runDate"];
	        this.horizonDays = source["horizonDays"];
	        this.n = source["n"];
	        this.portRet = source["portRet"];
	        this.benchRet = source["benchRet"];
	        this.excess = source["excess"];
	        this.matured = source["matured"];
	        this.daysElapsed = source["daysElapsed"];
	    }
	}
	export class CompositeValidationResult {
	    rows: CompositeValidationRow[];
	    costNote: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new CompositeValidationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], CompositeValidationRow);
	        this.costNote = source["costNote"];
	        this.warning = source["warning"];
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
	
	
	export class EquityPledge {
	    records?: any[];
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new EquityPledge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.latest = source["latest"];
	    }
	}
	export class F10CapitalOperation {
	    raiseSources?: any[];
	    projectProgress?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10CapitalOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.raiseSources = source["raiseSources"];
	        this.projectProgress = source["projectProgress"];
	    }
	}
	export class F10CoreThemes {
	    boardTypes?: any[];
	    themes?: any[];
	    history?: any[];
	    selectedBoardReasons?: any[];
	    popularLeaders?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10CoreThemes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.boardTypes = source["boardTypes"];
	        this.themes = source["themes"];
	        this.history = source["history"];
	        this.selectedBoardReasons = source["selectedBoardReasons"];
	        this.popularLeaders = source["popularLeaders"];
	    }
	}
	export class F10EquityStructure {
	    latest?: any[];
	    history?: any[];
	    composition?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10EquityStructure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.latest = source["latest"];
	        this.history = source["history"];
	        this.composition = source["composition"];
	    }
	}
	export class F10IndustryCompareMetrics {
	    valuation?: any[];
	    performance?: any[];
	    growth?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10IndustryCompareMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valuation = source["valuation"];
	        this.performance = source["performance"];
	        this.growth = source["growth"];
	    }
	}
	export class F10MainIndicators {
	    latest?: any[];
	    yearly?: any[];
	    quarterly?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10MainIndicators(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.latest = source["latest"];
	        this.yearly = source["yearly"];
	        this.quarterly = source["quarterly"];
	    }
	}
	export class F10Management {
	    managementList?: any[];
	    salaryDetails?: any[];
	    holdingChanges?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10Management(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.managementList = source["managementList"];
	        this.salaryDetails = source["salaryDetails"];
	        this.holdingChanges = source["holdingChanges"];
	    }
	}
	export class F10OperationsRequired {
	    latestIndicators?: Record<string, any>;
	    latestIndicatorsExtra?: Record<string, any>;
	    latestIndicatorsQuote?: Record<string, any>;
	    eventReminders?: any[];
	    news?: any[];
	    announcements?: any[];
	    shareholderAnalysis?: any[];
	    dragonTigerList?: any[];
	    blockTrades?: any[];
	    marginTrading?: any[];
	    mainIndicators?: any[];
	    sectorTags?: any[];
	    coreThemes?: any[];
	    institutionForecast?: any[];
	    forecastChart?: any[];
	    reportSummary?: any[];
	    researchReports?: any[];
	    forecastRevisionTrack?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10OperationsRequired(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.latestIndicators = source["latestIndicators"];
	        this.latestIndicatorsExtra = source["latestIndicatorsExtra"];
	        this.latestIndicatorsQuote = source["latestIndicatorsQuote"];
	        this.eventReminders = source["eventReminders"];
	        this.news = source["news"];
	        this.announcements = source["announcements"];
	        this.shareholderAnalysis = source["shareholderAnalysis"];
	        this.dragonTigerList = source["dragonTigerList"];
	        this.blockTrades = source["blockTrades"];
	        this.marginTrading = source["marginTrading"];
	        this.mainIndicators = source["mainIndicators"];
	        this.sectorTags = source["sectorTags"];
	        this.coreThemes = source["coreThemes"];
	        this.institutionForecast = source["institutionForecast"];
	        this.forecastChart = source["forecastChart"];
	        this.reportSummary = source["reportSummary"];
	        this.researchReports = source["researchReports"];
	        this.forecastRevisionTrack = source["forecastRevisionTrack"];
	    }
	}
	export class F10ValuationTrend {
	    source?: string;
	    range?: string;
	    requestedRange?: string;
	    fallback?: boolean;
	    dateType?: number;
	    labels?: Record<string, string>;
	    pe?: any[];
	    pb?: any[];
	    ps?: any[];
	    pcf?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10ValuationTrend(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.range = source["range"];
	        this.requestedRange = source["requestedRange"];
	        this.fallback = source["fallback"];
	        this.dateType = source["dateType"];
	        this.labels = source["labels"];
	        this.pe = source["pe"];
	        this.pb = source["pb"];
	        this.ps = source["ps"];
	        this.pcf = source["pcf"];
	    }
	}
	export class F10RelatedStocks {
	    industryRankings?: any[];
	    conceptRelations?: any[];
	
	    static createFrom(source: any = {}) {
	        return new F10RelatedStocks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.industryRankings = source["industryRankings"];
	        this.conceptRelations = source["conceptRelations"];
	    }
	}
	export class StockValuation {
	    price?: number;
	    peTtm?: number;
	    pb?: number;
	    totalMarketCap?: number;
	    floatMarketCap?: number;
	    turnoverRate?: number;
	    amplitude?: number;
	    totalShares?: number;
	    floatShares?: number;
	
	    static createFrom(source: any = {}) {
	        return new StockValuation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.price = source["price"];
	        this.peTtm = source["peTtm"];
	        this.pb = source["pb"];
	        this.totalMarketCap = source["totalMarketCap"];
	        this.floatMarketCap = source["floatMarketCap"];
	        this.turnoverRate = source["turnoverRate"];
	        this.amplitude = source["amplitude"];
	        this.totalShares = source["totalShares"];
	        this.floatShares = source["floatShares"];
	    }
	}
	export class StockBuyback {
	    records?: any[];
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new StockBuyback(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.latest = source["latest"];
	    }
	}
	export class ShareholderChanges {
	    records?: any[];
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ShareholderChanges(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.latest = source["latest"];
	    }
	}
	export class LockupRelease {
	    records?: any[];
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new LockupRelease(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.latest = source["latest"];
	    }
	}
	export class ShareholderNumbers {
	    records?: any[];
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ShareholderNumbers(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.latest = source["latest"];
	    }
	}
	export class StockPeer {
	    symbol: string;
	    name: string;
	    market?: string;
	
	    static createFrom(source: any = {}) {
	        return new StockPeer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.market = source["market"];
	    }
	}
	export class IndustryCompare {
	    industry?: string;
	    peers?: StockPeer[];
	
	    static createFrom(source: any = {}) {
	        return new IndustryCompare(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.industry = source["industry"];
	        this.peers = this.convertValues(source["peers"], StockPeer);
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
	export class InstitutionalHoldings {
	    topHolders?: any[];
	    controller?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new InstitutionalHoldings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.topHolders = source["topHolders"];
	        this.controller = source["controller"];
	    }
	}
	export class FundFlowSeries {
	    fields?: string[];
	    lines?: string[][];
	    labels?: Record<string, string>;
	    latest?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new FundFlowSeries(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = source["fields"];
	        this.lines = source["lines"];
	        this.labels = source["labels"];
	        this.latest = source["latest"];
	    }
	}
	export class PerformanceEvents {
	    forecast?: any[];
	    express?: any[];
	    schedule?: any[];
	
	    static createFrom(source: any = {}) {
	        return new PerformanceEvents(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.forecast = source["forecast"];
	        this.express = source["express"];
	        this.schedule = source["schedule"];
	    }
	}
	export class FinancialStatements {
	    income?: any[];
	    balance?: any[];
	    cashflow?: any[];
	
	    static createFrom(source: any = {}) {
	        return new FinancialStatements(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.income = source["income"];
	        this.balance = source["balance"];
	        this.cashflow = source["cashflow"];
	    }
	}
	export class F10Overview {
	    code: string;
	    updatedAt?: string;
	    source?: string;
	    company?: Record<string, any>;
	    financials?: FinancialStatements;
	    performance?: PerformanceEvents;
	    fundFlow?: FundFlowSeries;
	    institutions?: InstitutionalHoldings;
	    industry?: IndustryCompare;
	    bonus?: BonusFinancing;
	    business?: BusinessAnalysis;
	    shareholders?: ShareholderNumbers;
	    pledge?: EquityPledge;
	    lockup?: LockupRelease;
	    holderChange?: ShareholderChanges;
	    buyback?: StockBuyback;
	    valuation?: StockValuation;
	    operations?: F10OperationsRequired;
	    coreThemes?: F10CoreThemes;
	    industryMetrics?: F10IndustryCompareMetrics;
	    mainIndicators?: F10MainIndicators;
	    management?: F10Management;
	    capitalOperation?: F10CapitalOperation;
	    equityStructure?: F10EquityStructure;
	    relatedStocks?: F10RelatedStocks;
	    valuationTrend?: F10ValuationTrend;
	    errors?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new F10Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.updatedAt = source["updatedAt"];
	        this.source = source["source"];
	        this.company = source["company"];
	        this.financials = this.convertValues(source["financials"], FinancialStatements);
	        this.performance = this.convertValues(source["performance"], PerformanceEvents);
	        this.fundFlow = this.convertValues(source["fundFlow"], FundFlowSeries);
	        this.institutions = this.convertValues(source["institutions"], InstitutionalHoldings);
	        this.industry = this.convertValues(source["industry"], IndustryCompare);
	        this.bonus = this.convertValues(source["bonus"], BonusFinancing);
	        this.business = this.convertValues(source["business"], BusinessAnalysis);
	        this.shareholders = this.convertValues(source["shareholders"], ShareholderNumbers);
	        this.pledge = this.convertValues(source["pledge"], EquityPledge);
	        this.lockup = this.convertValues(source["lockup"], LockupRelease);
	        this.holderChange = this.convertValues(source["holderChange"], ShareholderChanges);
	        this.buyback = this.convertValues(source["buyback"], StockBuyback);
	        this.valuation = this.convertValues(source["valuation"], StockValuation);
	        this.operations = this.convertValues(source["operations"], F10OperationsRequired);
	        this.coreThemes = this.convertValues(source["coreThemes"], F10CoreThemes);
	        this.industryMetrics = this.convertValues(source["industryMetrics"], F10IndustryCompareMetrics);
	        this.mainIndicators = this.convertValues(source["mainIndicators"], F10MainIndicators);
	        this.management = this.convertValues(source["management"], F10Management);
	        this.capitalOperation = this.convertValues(source["capitalOperation"], F10CapitalOperation);
	        this.equityStructure = this.convertValues(source["equityStructure"], F10EquityStructure);
	        this.relatedStocks = this.convertValues(source["relatedStocks"], F10RelatedStocks);
	        this.valuationTrend = this.convertValues(source["valuationTrend"], F10ValuationTrend);
	        this.errors = source["errors"];
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
	
	
	
	
	
	export class FundamentalCandidate {
	    symbol: string;
	    name: string;
	    price: number;
	    marketCapYi: number;
	    annRoe: number;
	    revYoY: number;
	    profitYoY: number;
	    grossMargin: number;
	    cfps: number;
	    eps: number;
	    amountYi: number;
	    debtRatio: number;
	    valPctile: number;
	    goodwillRatio: number;
	    dividendYield: number;
	    score: number;
	    passed: string[];
	
	    static createFrom(source: any = {}) {
	        return new FundamentalCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.marketCapYi = source["marketCapYi"];
	        this.annRoe = source["annRoe"];
	        this.revYoY = source["revYoY"];
	        this.profitYoY = source["profitYoY"];
	        this.grossMargin = source["grossMargin"];
	        this.cfps = source["cfps"];
	        this.eps = source["eps"];
	        this.amountYi = source["amountYi"];
	        this.debtRatio = source["debtRatio"];
	        this.valPctile = source["valPctile"];
	        this.goodwillRatio = source["goodwillRatio"];
	        this.dividendYield = source["dividendYield"];
	        this.score = source["score"];
	        this.passed = source["passed"];
	    }
	}
	export class FundamentalScanResult {
	    preset: string;
	    presetLabel: string;
	    reportDate: string;
	    universeCount: number;
	    candidates: FundamentalCandidate[];
	    warning: string;
	    rulesText: string;
	
	    static createFrom(source: any = {}) {
	        return new FundamentalScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preset = source["preset"];
	        this.presetLabel = source["presetLabel"];
	        this.reportDate = source["reportDate"];
	        this.universeCount = source["universeCount"];
	        this.candidates = this.convertValues(source["candidates"], FundamentalCandidate);
	        this.warning = source["warning"];
	        this.rulesText = source["rulesText"];
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
	export class StockPosition {
	    shares: number;
	    costPrice: number;
	    buyDate?: string;
	
	    static createFrom(source: any = {}) {
	        return new StockPosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shares = source["shares"];
	        this.costPrice = source["costPrice"];
	        this.buyDate = source["buyDate"];
	    }
	}
	export class HeldPosition {
	    stockCode: string;
	    stockName: string;
	    position: StockPosition;
	
	    static createFrom(source: any = {}) {
	        return new HeldPosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.position = this.convertValues(source["position"], StockPosition);
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
	export class HistoryAutoCollectRequest {
	    enabled: boolean;
	    collectStart: string;
	    collectEnd: string;
	    includeBeijing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryAutoCollectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.collectStart = source["collectStart"];
	        this.collectEnd = source["collectEnd"];
	        this.includeBeijing = source["includeBeijing"];
	    }
	}
	export class HistoryAutoCollectStatus {
	    enabled: boolean;
	    collectStart: string;
	    collectEnd: string;
	    includeBeijing: boolean;
	    lastCollectDate: string;
	    dbPath: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryAutoCollectStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.collectStart = source["collectStart"];
	        this.collectEnd = source["collectEnd"];
	        this.includeBeijing = source["includeBeijing"];
	        this.lastCollectDate = source["lastCollectDate"];
	        this.dbPath = source["dbPath"];
	        this.message = source["message"];
	    }
	}
	export class HistoryBackfillRequest {
	    codes: string[];
	    days: number;
	    throttleMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryBackfillRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.codes = source["codes"];
	        this.days = source["days"];
	        this.throttleMs = source["throttleMs"];
	    }
	}
	export class HistoryBackfillResult {
	    startedAt: string;
	    finishedAt: string;
	    dbPath: string;
	    totalCodes: number;
	    okCodes: number;
	    failedCodes: number;
	    savedRows: number;
	    earliestDate: string;
	    latestDate: string;
	    status: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryBackfillResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.dbPath = source["dbPath"];
	        this.totalCodes = source["totalCodes"];
	        this.okCodes = source["okCodes"];
	        this.failedCodes = source["failedCodes"];
	        this.savedRows = source["savedRows"];
	        this.earliestDate = source["earliestDate"];
	        this.latestDate = source["latestDate"];
	        this.status = source["status"];
	        this.message = source["message"];
	    }
	}
	export class HistoryCollectRequest {
	    tradeDate: string;
	    includeBeijing: boolean;
	    triggeredByAuto?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryCollectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tradeDate = source["tradeDate"];
	        this.includeBeijing = source["includeBeijing"];
	        this.triggeredByAuto = source["triggeredByAuto"];
	    }
	}
	export class HistoryCollectResult {
	    tradeDate: string;
	    startedAt: string;
	    finishedAt: string;
	    dbPath: string;
	    totalCount: number;
	    savedCount: number;
	    failedCount: number;
	    maUpdated: boolean;
	    status: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryCollectResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tradeDate = source["tradeDate"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.dbPath = source["dbPath"];
	        this.totalCount = source["totalCount"];
	        this.savedCount = source["savedCount"];
	        this.failedCount = source["failedCount"];
	        this.maUpdated = source["maUpdated"];
	        this.status = source["status"];
	        this.message = source["message"];
	    }
	}
	
	
	
	
	
	export class KLineData {
	    time: string;
	    open: number;
	    high: number;
	    low: number;
	    close: number;
	    volume: number;
	    amount?: number;
	    avg?: number;
	    ma5?: number;
	    ma10?: number;
	    ma20?: number;
	
	    static createFrom(source: any = {}) {
	        return new KLineData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.open = source["open"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.close = source["close"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	        this.avg = source["avg"];
	        this.ma5 = source["ma5"];
	        this.ma10 = source["ma10"];
	        this.ma20 = source["ma20"];
	    }
	}
	export class LateDayChaseScannerItem {
	    symbol: string;
	    name: string;
	    rank: number;
	    price: number;
	    changePercent: number;
	    volumeRatio: number;
	    turnoverRate: number;
	    amount: number;
	    totalMarketCap: number;
	    floatMarketCap: number;
	    industry: string;
	    score: number;
	    volumeStepPassed: boolean;
	    maBullishPassed: boolean;
	    intradayStrengthPassed: boolean;
	    buySignalReady: boolean;
	    intradayAboveAvgRatio: number;
	    stockIntradayReturn: number;
	    indexIntradayReturn: number;
	    ma5: number;
	    ma10: number;
	    ma20: number;
	    lastHighTime: string;
	    triggers: string[];
	    reasons: string[];
	    riskFlags: string[];
	    buyPointHint: string;
	    stopLossHint: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new LateDayChaseScannerItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.rank = source["rank"];
	        this.price = source["price"];
	        this.changePercent = source["changePercent"];
	        this.volumeRatio = source["volumeRatio"];
	        this.turnoverRate = source["turnoverRate"];
	        this.amount = source["amount"];
	        this.totalMarketCap = source["totalMarketCap"];
	        this.floatMarketCap = source["floatMarketCap"];
	        this.industry = source["industry"];
	        this.score = source["score"];
	        this.volumeStepPassed = source["volumeStepPassed"];
	        this.maBullishPassed = source["maBullishPassed"];
	        this.intradayStrengthPassed = source["intradayStrengthPassed"];
	        this.buySignalReady = source["buySignalReady"];
	        this.intradayAboveAvgRatio = source["intradayAboveAvgRatio"];
	        this.stockIntradayReturn = source["stockIntradayReturn"];
	        this.indexIntradayReturn = source["indexIntradayReturn"];
	        this.ma5 = source["ma5"];
	        this.ma10 = source["ma10"];
	        this.ma20 = source["ma20"];
	        this.lastHighTime = source["lastHighTime"];
	        this.triggers = source["triggers"];
	        this.reasons = source["reasons"];
	        this.riskFlags = source["riskFlags"];
	        this.buyPointHint = source["buyPointHint"];
	        this.stopLossHint = source["stopLossHint"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class LateDayChaseScannerRequest {
	    limit: number;
	    rankLimit: number;
	    includeBeijing: boolean;
	    minChangePct: number;
	    maxChangePct: number;
	    minVolumeRatio: number;
	    minTurnoverRate: number;
	    maxTurnoverRate: number;
	    minFloatCap: number;
	    maxFloatCap: number;
	    requireBuySignal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LateDayChaseScannerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.limit = source["limit"];
	        this.rankLimit = source["rankLimit"];
	        this.includeBeijing = source["includeBeijing"];
	        this.minChangePct = source["minChangePct"];
	        this.maxChangePct = source["maxChangePct"];
	        this.minVolumeRatio = source["minVolumeRatio"];
	        this.minTurnoverRate = source["minTurnoverRate"];
	        this.maxTurnoverRate = source["maxTurnoverRate"];
	        this.minFloatCap = source["minFloatCap"];
	        this.maxFloatCap = source["maxFloatCap"];
	        this.requireBuySignal = source["requireBuySignal"];
	    }
	}
	export class LateDayChaseScannerResult {
	    asOf: string;
	    ruleVersion: string;
	    universeCount: number;
	    rankLimit: number;
	    rankedCount: number;
	    candidateCount: number;
	    selectedCount: number;
	    items: LateDayChaseScannerItem[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new LateDayChaseScannerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asOf = source["asOf"];
	        this.ruleVersion = source["ruleVersion"];
	        this.universeCount = source["universeCount"];
	        this.rankLimit = source["rankLimit"];
	        this.rankedCount = source["rankedCount"];
	        this.candidateCount = source["candidateCount"];
	        this.selectedCount = source["selectedCount"];
	        this.items = this.convertValues(source["items"], LateDayChaseScannerItem);
	        this.warning = source["warning"];
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
	
	
	export class LongHuBangDetail {
	    rank: number;
	    operName: string;
	    buyAmt: number;
	    buyPercent: number;
	    sellAmt: number;
	    sellPercent: number;
	    netAmt: number;
	    direction: string;
	
	    static createFrom(source: any = {}) {
	        return new LongHuBangDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rank = source["rank"];
	        this.operName = source["operName"];
	        this.buyAmt = source["buyAmt"];
	        this.buyPercent = source["buyPercent"];
	        this.sellAmt = source["sellAmt"];
	        this.sellPercent = source["sellPercent"];
	        this.netAmt = source["netAmt"];
	        this.direction = source["direction"];
	    }
	}
	export class LongHuBangItem {
	    tradeDate: string;
	    code: string;
	    secuCode: string;
	    name: string;
	    closePrice: number;
	    changePercent: number;
	    netBuyAmt: number;
	    buyAmt: number;
	    sellAmt: number;
	    totalAmt: number;
	    turnoverRate: number;
	    freeCap: number;
	    reason: string;
	    reasonDetail: string;
	    accumAmount: number;
	    dealRatio: number;
	    netRatio: number;
	    d1Change: number;
	    d2Change: number;
	    d5Change: number;
	    d10Change: number;
	    securityType: string;
	
	    static createFrom(source: any = {}) {
	        return new LongHuBangItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tradeDate = source["tradeDate"];
	        this.code = source["code"];
	        this.secuCode = source["secuCode"];
	        this.name = source["name"];
	        this.closePrice = source["closePrice"];
	        this.changePercent = source["changePercent"];
	        this.netBuyAmt = source["netBuyAmt"];
	        this.buyAmt = source["buyAmt"];
	        this.sellAmt = source["sellAmt"];
	        this.totalAmt = source["totalAmt"];
	        this.turnoverRate = source["turnoverRate"];
	        this.freeCap = source["freeCap"];
	        this.reason = source["reason"];
	        this.reasonDetail = source["reasonDetail"];
	        this.accumAmount = source["accumAmount"];
	        this.dealRatio = source["dealRatio"];
	        this.netRatio = source["netRatio"];
	        this.d1Change = source["d1Change"];
	        this.d2Change = source["d2Change"];
	        this.d5Change = source["d5Change"];
	        this.d10Change = source["d10Change"];
	        this.securityType = source["securityType"];
	    }
	}
	export class LowBuyBatchRow {
	    label: string;
	    trades: number;
	    winRate: number;
	    expectancy: number;
	    payoffRatio: number;
	    profitFactor: number;
	    maxLoss: number;
	    benchmark: number;
	    excess: number;
	    avgHold: number;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyBatchRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.trades = source["trades"];
	        this.winRate = source["winRate"];
	        this.expectancy = source["expectancy"];
	        this.payoffRatio = source["payoffRatio"];
	        this.profitFactor = source["profitFactor"];
	        this.maxLoss = source["maxLoss"];
	        this.benchmark = source["benchmark"];
	        this.excess = source["excess"];
	        this.avgHold = source["avgHold"];
	    }
	}
	export class LowBuyBatchResult {
	    start: string;
	    end: string;
	    topN: number;
	    rows: LowBuyBatchRow[];
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyBatchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.topN = source["topN"];
	        this.rows = this.convertValues(source["rows"], LowBuyBatchRow);
	        this.warning = source["warning"];
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
	
	export class LowBuyMarketOverview {
	    shPrice: number;
	    shMA20: number;
	    limitUpCount: number;
	    limitDownCount: number;
	    totalAmount: number;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyMarketOverview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shPrice = source["shPrice"];
	        this.shMA20 = source["shMA20"];
	        this.limitUpCount = source["limitUpCount"];
	        this.limitDownCount = source["limitDownCount"];
	        this.totalAmount = source["totalAmount"];
	    }
	}
	export class LowBuyScannerItem {
	    symbol: string;
	    name: string;
	    price: number;
	    changePercent: number;
	    amount: number;
	    turnoverRate: number;
	    mainNetInflow: number;
	    mainNetInflowRatio: number;
	    mainFlowSource: string;
	    totalMarketCap: number;
	    floatMarketCap: number;
	    capBucket: string;
	    industry: string;
	    score: number;
	    triggerCount: number;
	    triggers: string[];
	    reasons: string[];
	    riskFlags: string[];
	    buyPointHint: string;
	    sellPointHint: string;
	    stopLossHint: string;
	    ma10: number;
	    ma10Status: string;
	    nextDate?: string;
	    nextOpenGainPct?: number;
	    nextHighGainPct?: number;
	    nextCloseGainPct?: number;
	    replayExitDate?: string;
	    replayHoldDays?: number;
	    replayReturnPct?: number;
	    replayExitReason?: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyScannerItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.changePercent = source["changePercent"];
	        this.amount = source["amount"];
	        this.turnoverRate = source["turnoverRate"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.mainNetInflowRatio = source["mainNetInflowRatio"];
	        this.mainFlowSource = source["mainFlowSource"];
	        this.totalMarketCap = source["totalMarketCap"];
	        this.floatMarketCap = source["floatMarketCap"];
	        this.capBucket = source["capBucket"];
	        this.industry = source["industry"];
	        this.score = source["score"];
	        this.triggerCount = source["triggerCount"];
	        this.triggers = source["triggers"];
	        this.reasons = source["reasons"];
	        this.riskFlags = source["riskFlags"];
	        this.buyPointHint = source["buyPointHint"];
	        this.sellPointHint = source["sellPointHint"];
	        this.stopLossHint = source["stopLossHint"];
	        this.ma10 = source["ma10"];
	        this.ma10Status = source["ma10Status"];
	        this.nextDate = source["nextDate"];
	        this.nextOpenGainPct = source["nextOpenGainPct"];
	        this.nextHighGainPct = source["nextHighGainPct"];
	        this.nextCloseGainPct = source["nextCloseGainPct"];
	        this.replayExitDate = source["replayExitDate"];
	        this.replayHoldDays = source["replayHoldDays"];
	        this.replayReturnPct = source["replayReturnPct"];
	        this.replayExitReason = source["replayExitReason"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class LowBuyScannerRequest {
	    limit: number;
	    includeBeijing: boolean;
	    historyFilterEnabled: boolean;
	    historyTurnoverDays: number;
	    historyTurnoverMax: number;
	    historyMainFlowDays: number;
	    historyMainFlowPositiveDays: number;
	    historyMAPeriod: number;
	    maxChangePct?: number;
	    caoYuanStrict?: boolean;
	    historyPickDate?: string;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyScannerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.limit = source["limit"];
	        this.includeBeijing = source["includeBeijing"];
	        this.historyFilterEnabled = source["historyFilterEnabled"];
	        this.historyTurnoverDays = source["historyTurnoverDays"];
	        this.historyTurnoverMax = source["historyTurnoverMax"];
	        this.historyMainFlowDays = source["historyMainFlowDays"];
	        this.historyMainFlowPositiveDays = source["historyMainFlowPositiveDays"];
	        this.historyMAPeriod = source["historyMAPeriod"];
	        this.maxChangePct = source["maxChangePct"];
	        this.caoYuanStrict = source["caoYuanStrict"];
	        this.historyPickDate = source["historyPickDate"];
	    }
	}
	export class LowBuyScannerResult {
	    asOf: string;
	    ruleVersion: string;
	    universeCount: number;
	    candidateCount: number;
	    selectedCount: number;
	    marketGatePassed: boolean;
	    marketGateReasons: string[];
	    marketOverview: LowBuyMarketOverview;
	    items: LowBuyScannerItem[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new LowBuyScannerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asOf = source["asOf"];
	        this.ruleVersion = source["ruleVersion"];
	        this.universeCount = source["universeCount"];
	        this.candidateCount = source["candidateCount"];
	        this.selectedCount = source["selectedCount"];
	        this.marketGatePassed = source["marketGatePassed"];
	        this.marketGateReasons = source["marketGateReasons"];
	        this.marketOverview = this.convertValues(source["marketOverview"], LowBuyMarketOverview);
	        this.items = this.convertValues(source["items"], LowBuyScannerItem);
	        this.warning = source["warning"];
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
	
	
	
	export class MarketChangeBin {
	    key: string;
	    label: string;
	    side: string;
	    count: number;
	    minPct?: number;
	    maxPct?: number;
	
	    static createFrom(source: any = {}) {
	        return new MarketChangeBin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.side = source["side"];
	        this.count = source["count"];
	        this.minPct = source["minPct"];
	        this.maxPct = source["maxPct"];
	    }
	}
	export class MarketChangeDistribution {
	    total: number;
	    upCount: number;
	    downCount: number;
	    flatCount: number;
	    limitUpCount: number;
	    limitDownCount: number;
	    bins: MarketChangeBin[];
	    updateTime?: string;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new MarketChangeDistribution(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.upCount = source["upCount"];
	        this.downCount = source["downCount"];
	        this.flatCount = source["flatCount"];
	        this.limitUpCount = source["limitUpCount"];
	        this.limitDownCount = source["limitDownCount"];
	        this.bins = this.convertValues(source["bins"], MarketChangeBin);
	        this.updateTime = source["updateTime"];
	        this.source = source["source"];
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
	export class MarketIndex {
	    code: string;
	    name: string;
	    price: number;
	    change: number;
	    changePercent: number;
	    volume: number;
	    amount: number;
	
	    static createFrom(source: any = {}) {
	        return new MarketIndex(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.change = source["change"];
	        this.changePercent = source["changePercent"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	    }
	}
	export class MarketRegime {
	    regime: string;
	    emoji: string;
	    label: string;
	    limitUp: number;
	    limitDown: number;
	    amountYi: number;
	    shPrice: number;
	    shMA20: number;
	    aboveMA20: boolean;
	    asOf: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MarketRegime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.regime = source["regime"];
	        this.emoji = source["emoji"];
	        this.label = source["label"];
	        this.limitUp = source["limitUp"];
	        this.limitDown = source["limitDown"];
	        this.amountYi = source["amountYi"];
	        this.shPrice = source["shPrice"];
	        this.shMA20 = source["shMA20"];
	        this.aboveMA20 = source["aboveMA20"];
	        this.asOf = source["asOf"];
	        this.available = source["available"];
	    }
	}
	export class MarketStyleItem {
	    key: string;
	    name: string;
	    indexName: string;
	    code: string;
	    changePercent: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new MarketStyleItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.indexName = source["indexName"];
	        this.code = source["code"];
	        this.changePercent = source["changePercent"];
	        this.source = source["source"];
	    }
	}
	export class MarketStylePreference {
	    label: string;
	    subLabel: string;
	    scenario: string;
	    strengthGap: number;
	    strongKey: string;
	    weakKey: string;
	    items: MarketStyleItem[];
	    asOf: string;
	    available: boolean;
	    dataNote?: string;
	    regimeFallback?: MarketRegime;
	
	    static createFrom(source: any = {}) {
	        return new MarketStylePreference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.subLabel = source["subLabel"];
	        this.scenario = source["scenario"];
	        this.strengthGap = source["strengthGap"];
	        this.strongKey = source["strongKey"];
	        this.weakKey = source["weakKey"];
	        this.items = this.convertValues(source["items"], MarketStyleItem);
	        this.asOf = source["asOf"];
	        this.available = source["available"];
	        this.dataNote = source["dataNote"];
	        this.regimeFallback = this.convertValues(source["regimeFallback"], MarketRegime);
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
	
	
	
	export class OrderBookItem {
	    price: number;
	    size: number;
	    total: number;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new OrderBookItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.price = source["price"];
	        this.size = source["size"];
	        this.total = source["total"];
	        this.percent = source["percent"];
	    }
	}
	export class OrderBook {
	    bids: OrderBookItem[];
	    asks: OrderBookItem[];
	
	    static createFrom(source: any = {}) {
	        return new OrderBook(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bids = this.convertValues(source["bids"], OrderBookItem);
	        this.asks = this.convertValues(source["asks"], OrderBookItem);
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
	
	export class PaperPosition {
	    id: number;
	    symbol: string;
	    name: string;
	    source: string;
	    costPrice: number;
	    shares: number;
	    openDate: string;
	    openPrice: number;
	    status: string;
	    closePrice: number;
	    closeDate: string;
	    exitReason: string;
	    currentPrice?: number;
	    profitPct?: number;
	    profitAmount?: number;
	    riskKind?: string;
	    stopPrice?: number;
	    tpPrice?: number;
	
	    static createFrom(source: any = {}) {
	        return new PaperPosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.costPrice = source["costPrice"];
	        this.shares = source["shares"];
	        this.openDate = source["openDate"];
	        this.openPrice = source["openPrice"];
	        this.status = source["status"];
	        this.closePrice = source["closePrice"];
	        this.closeDate = source["closeDate"];
	        this.exitReason = source["exitReason"];
	        this.currentPrice = source["currentPrice"];
	        this.profitPct = source["profitPct"];
	        this.profitAmount = source["profitAmount"];
	        this.riskKind = source["riskKind"];
	        this.stopPrice = source["stopPrice"];
	        this.tpPrice = source["tpPrice"];
	    }
	}
	export class RiskConcentration {
	    name: string;
	    pct: number;
	
	    static createFrom(source: any = {}) {
	        return new RiskConcentration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.pct = source["pct"];
	    }
	}
	export class PaperRiskSummary {
	    positionCount: number;
	    totalCost: number;
	    totalValue: number;
	    profitPct: number;
	    singleCap: number;
	    sectorCap: number;
	    drawdownAlertPct: number;
	    maxSinglePct: number;
	    singleOver: RiskConcentration[];
	    sectorTop: RiskConcentration[];
	    sectorOver: RiskConcentration[];
	    peakValue: number;
	    drawdownFromPeak: number;
	    drawdownAlert: boolean;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new PaperRiskSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.positionCount = source["positionCount"];
	        this.totalCost = source["totalCost"];
	        this.totalValue = source["totalValue"];
	        this.profitPct = source["profitPct"];
	        this.singleCap = source["singleCap"];
	        this.sectorCap = source["sectorCap"];
	        this.drawdownAlertPct = source["drawdownAlertPct"];
	        this.maxSinglePct = source["maxSinglePct"];
	        this.singleOver = this.convertValues(source["singleOver"], RiskConcentration);
	        this.sectorTop = this.convertValues(source["sectorTop"], RiskConcentration);
	        this.sectorOver = this.convertValues(source["sectorOver"], RiskConcentration);
	        this.peakValue = source["peakValue"];
	        this.drawdownFromPeak = source["drawdownFromPeak"];
	        this.drawdownAlert = source["drawdownAlert"];
	        this.warnings = source["warnings"];
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
	export class PaperSourceStat {
	    source: string;
	    total: number;
	    closed: number;
	    win: number;
	    winRate: number;
	    avgReturn: number;
	    totalReturn: number;
	    avgWin: number;
	    avgLoss: number;
	    payoffRatio: number;
	    profitFactor: number;
	    maxLoss: number;
	
	    static createFrom(source: any = {}) {
	        return new PaperSourceStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.total = source["total"];
	        this.closed = source["closed"];
	        this.win = source["win"];
	        this.winRate = source["winRate"];
	        this.avgReturn = source["avgReturn"];
	        this.totalReturn = source["totalReturn"];
	        this.avgWin = source["avgWin"];
	        this.avgLoss = source["avgLoss"];
	        this.payoffRatio = source["payoffRatio"];
	        this.profitFactor = source["profitFactor"];
	        this.maxLoss = source["maxLoss"];
	    }
	}
	export class PaperStats {
	    openCount: number;
	    closedCount: number;
	    winRate: number;
	    expectancy: number;
	    payoffRatio: number;
	    profitFactor: number;
	    maxLoss: number;
	    bySource: PaperSourceStat[];
	
	    static createFrom(source: any = {}) {
	        return new PaperStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openCount = source["openCount"];
	        this.closedCount = source["closedCount"];
	        this.winRate = source["winRate"];
	        this.expectancy = source["expectancy"];
	        this.payoffRatio = source["payoffRatio"];
	        this.profitFactor = source["profitFactor"];
	        this.maxLoss = source["maxLoss"];
	        this.bySource = this.convertValues(source["bySource"], PaperSourceStat);
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
	
	
	
	export class PushResult {
	    sent: boolean;
	    skipped: boolean;
	    channels: Record<string, string>;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new PushResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sent = source["sent"];
	        this.skipped = source["skipped"];
	        this.channels = source["channels"];
	        this.message = source["message"];
	    }
	}
	export class PushSignal {
	    stockCode: string;
	    stockName: string;
	    type: string;
	    message: string;
	    level?: string;
	
	    static createFrom(source: any = {}) {
	        return new PushSignal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.type = source["type"];
	        this.message = source["message"];
	        this.level = source["level"];
	    }
	}
	
	
	
	
	
	export class Stock {
	    symbol: string;
	    name: string;
	    price: number;
	    change: number;
	    changePercent: number;
	    volume: number;
	    amount: number;
	    marketCap: string;
	    sector: string;
	    open: number;
	    high: number;
	    low: number;
	    preClose: number;
	
	    static createFrom(source: any = {}) {
	        return new Stock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.change = source["change"];
	        this.changePercent = source["changePercent"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	        this.marketCap = source["marketCap"];
	        this.sector = source["sector"];
	        this.open = source["open"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.preClose = source["preClose"];
	    }
	}
	
	export class StockGroup {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new StockGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class StockMoveItem {
	    rank: number;
	    code: string;
	    name: string;
	    price: number;
	    changePercent: number;
	    speed: number;
	    turnoverRate: number;
	    volume: number;
	    amount: number;
	    mainNetInflow: number;
	    mainNetInflowRatio: number;
	    high: number;
	    low: number;
	    open: number;
	    preClose: number;
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new StockMoveItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rank = source["rank"];
	        this.code = source["code"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.changePercent = source["changePercent"];
	        this.speed = source["speed"];
	        this.turnoverRate = source["turnoverRate"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.mainNetInflowRatio = source["mainNetInflowRatio"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.open = source["open"];
	        this.preClose = source["preClose"];
	        this.updateTime = source["updateTime"];
	    }
	}
	export class StockMoveList {
	    moveType: string;
	    items: StockMoveItem[];
	    total?: number;
	    updateTime?: string;
	
	    static createFrom(source: any = {}) {
	        return new StockMoveList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.moveType = source["moveType"];
	        this.items = this.convertValues(source["items"], StockMoveItem);
	        this.total = source["total"];
	        this.updateTime = source["updateTime"];
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
	
	
	export class StockSession {
	    id: string;
	    stockCode: string;
	    stockName: string;
	    messages: ChatMessage[];
	    position?: StockPosition;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new StockSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.messages = this.convertValues(source["messages"], ChatMessage);
	        this.position = this.convertValues(source["position"], StockPosition);
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
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
	
	export class StrategyAgent {
	    id: string;
	    name: string;
	    role: string;
	    avatar: string;
	    color: string;
	    instruction: string;
	    tools: string[];
	    mcpServers: string[];
	    enabled: boolean;
	    aiConfigId: string;
	
	    static createFrom(source: any = {}) {
	        return new StrategyAgent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.role = source["role"];
	        this.avatar = source["avatar"];
	        this.color = source["color"];
	        this.instruction = source["instruction"];
	        this.tools = source["tools"];
	        this.mcpServers = source["mcpServers"];
	        this.enabled = source["enabled"];
	        this.aiConfigId = source["aiConfigId"];
	    }
	}
	export class Strategy {
	    id: string;
	    name: string;
	    description: string;
	    color: string;
	    agents: StrategyAgent[];
	    isBuiltin: boolean;
	    source: string;
	    sourceMeta: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Strategy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.agents = this.convertValues(source["agents"], StrategyAgent);
	        this.isBuiltin = source["isBuiltin"];
	        this.source = source["source"];
	        this.sourceMeta = source["sourceMeta"];
	        this.createdAt = source["createdAt"];
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
	export class StrategyAccountResult {
	    strategy: string;
	    capital: number;
	    startDate: string;
	    endDate: string;
	    finalEquity: number;
	    returnPct: number;
	    maxDrawdown: number;
	    benchmark: number;
	    excess: number;
	    cash: number;
	    closedTrades: number;
	    winRate: number;
	    expectancy: number;
	    payoffRatio: number;
	    profitFactor: number;
	    avgHoldDays: number;
	    holdings: AccountHolding[];
	    trades: AccountTrade[];
	    equity: AccountEquityPoint[];
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new StrategyAccountResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategy = source["strategy"];
	        this.capital = source["capital"];
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.finalEquity = source["finalEquity"];
	        this.returnPct = source["returnPct"];
	        this.maxDrawdown = source["maxDrawdown"];
	        this.benchmark = source["benchmark"];
	        this.excess = source["excess"];
	        this.cash = source["cash"];
	        this.closedTrades = source["closedTrades"];
	        this.winRate = source["winRate"];
	        this.expectancy = source["expectancy"];
	        this.payoffRatio = source["payoffRatio"];
	        this.profitFactor = source["profitFactor"];
	        this.avgHoldDays = source["avgHoldDays"];
	        this.holdings = this.convertValues(source["holdings"], AccountHolding);
	        this.trades = this.convertValues(source["trades"], AccountTrade);
	        this.equity = this.convertValues(source["equity"], AccountEquityPoint);
	        this.warning = source["warning"];
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
	
	export class StrategyReviewNews {
	    time: string;
	    content: string;
	    url?: string;
	
	    static createFrom(source: any = {}) {
	        return new StrategyReviewNews(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.content = source["content"];
	        this.url = source["url"];
	    }
	}
	export class StrategyReviewItem {
	    symbol: string;
	    name: string;
	    rank: number;
	    industry: string;
	    businessSummary?: string;
	    businessSource?: string;
	    signalPrice: number;
	    signalChangePercent: number;
	    signalScore: number;
	    signalReasons: string[];
	    signalTriggers: string[];
	    signalRisks: string[];
	    reviewDate: string;
	    open: number;
	    high: number;
	    low: number;
	    close: number;
	    dayChangePercent: number;
	    closeReturnPercent: number;
	    highReturnPercent: number;
	    turnoverRate: number;
	    amount: number;
	    mainNetInflow: number;
	    mainNetInflowPct: number;
	    mainFlowSource: string;
	    klineSummary: string;
	    fundSummary: string;
	    outcome: string;
	    suggestions: string[];
	    news: StrategyReviewNews[];
	
	    static createFrom(source: any = {}) {
	        return new StrategyReviewItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.rank = source["rank"];
	        this.industry = source["industry"];
	        this.businessSummary = source["businessSummary"];
	        this.businessSource = source["businessSource"];
	        this.signalPrice = source["signalPrice"];
	        this.signalChangePercent = source["signalChangePercent"];
	        this.signalScore = source["signalScore"];
	        this.signalReasons = source["signalReasons"];
	        this.signalTriggers = source["signalTriggers"];
	        this.signalRisks = source["signalRisks"];
	        this.reviewDate = source["reviewDate"];
	        this.open = source["open"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.close = source["close"];
	        this.dayChangePercent = source["dayChangePercent"];
	        this.closeReturnPercent = source["closeReturnPercent"];
	        this.highReturnPercent = source["highReturnPercent"];
	        this.turnoverRate = source["turnoverRate"];
	        this.amount = source["amount"];
	        this.mainNetInflow = source["mainNetInflow"];
	        this.mainNetInflowPct = source["mainNetInflowPct"];
	        this.mainFlowSource = source["mainFlowSource"];
	        this.klineSummary = source["klineSummary"];
	        this.fundSummary = source["fundSummary"];
	        this.outcome = source["outcome"];
	        this.suggestions = source["suggestions"];
	        this.news = this.convertValues(source["news"], StrategyReviewNews);
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
	export class StrategyReviewMarket {
	    reviewDate: string;
	    shPrice: number;
	    shChangePercent: number;
	    limitUpCount: number;
	    limitDownCount: number;
	    totalAmount: number;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new StrategyReviewMarket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reviewDate = source["reviewDate"];
	        this.shPrice = source["shPrice"];
	        this.shChangePercent = source["shChangePercent"];
	        this.limitUpCount = source["limitUpCount"];
	        this.limitDownCount = source["limitDownCount"];
	        this.totalAmount = source["totalAmount"];
	        this.summary = source["summary"];
	    }
	}
	
	export class StrategyReviewRequest {
	    strategyId: string;
	    strategyName?: string;
	    signalDate?: string;
	    reviewDate?: string;
	    reviewSymbols?: string[];
	
	    static createFrom(source: any = {}) {
	        return new StrategyReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategyId = source["strategyId"];
	        this.strategyName = source["strategyName"];
	        this.signalDate = source["signalDate"];
	        this.reviewDate = source["reviewDate"];
	        this.reviewSymbols = source["reviewSymbols"];
	    }
	}
	export class StrategyReviewResult {
	    strategyId: string;
	    strategyName: string;
	    signalDate: string;
	    reviewDate: string;
	    generatedAt: string;
	    pickCount: number;
	    reviewedCount: number;
	    winRate: number;
	    avgCloseReturn: number;
	    avgHighReturn: number;
	    hit3Rate: number;
	    market: StrategyReviewMarket;
	    news: StrategyReviewNews[];
	    items: StrategyReviewItem[];
	    optimization: string[];
	    warning?: string;
	    dataSourceNotes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new StrategyReviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategyId = source["strategyId"];
	        this.strategyName = source["strategyName"];
	        this.signalDate = source["signalDate"];
	        this.reviewDate = source["reviewDate"];
	        this.generatedAt = source["generatedAt"];
	        this.pickCount = source["pickCount"];
	        this.reviewedCount = source["reviewedCount"];
	        this.winRate = source["winRate"];
	        this.avgCloseReturn = source["avgCloseReturn"];
	        this.avgHighReturn = source["avgHighReturn"];
	        this.hit3Rate = source["hit3Rate"];
	        this.market = this.convertValues(source["market"], StrategyReviewMarket);
	        this.news = this.convertValues(source["news"], StrategyReviewNews);
	        this.items = this.convertValues(source["items"], StrategyReviewItem);
	        this.optimization = source["optimization"];
	        this.warning = source["warning"];
	        this.dataSourceNotes = source["dataSourceNotes"];
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
	export class TailForwardCandidate {
	    symbol: string;
	    name: string;
	    source: string;
	    sourceLabel: string;
	    price: number;
	    changePct: number;
	    score: number;
	    buyable: boolean;
	    reason: string;
	    alreadyHeld: boolean;
	    added: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TailForwardCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.sourceLabel = source["sourceLabel"];
	        this.price = source["price"];
	        this.changePct = source["changePct"];
	        this.score = source["score"];
	        this.buyable = source["buyable"];
	        this.reason = source["reason"];
	        this.alreadyHeld = source["alreadyHeld"];
	        this.added = source["added"];
	    }
	}
	
	export class TailForwardResult {
	    asOf: string;
	    strategy: string;
	    auto: boolean;
	    candidates: TailForwardCandidate[];
	    buyableCount: number;
	    sealedCount: number;
	    addedCount: number;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new TailForwardResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asOf = source["asOf"];
	        this.strategy = source["strategy"];
	        this.auto = source["auto"];
	        this.candidates = this.convertValues(source["candidates"], TailForwardCandidate);
	        this.buyableCount = source["buyableCount"];
	        this.sealedCount = source["sealedCount"];
	        this.addedCount = source["addedCount"];
	        this.warning = source["warning"];
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
	export class TailLazyBatchRow {
	    label: string;
	    samples: number;
	    hit3Rate: number;
	    hit5Rate: number;
	    avgHigh: number;
	    avgOpen: number;
	    avgClose: number;
	    tpWinRate: number;
	    tpExpectancy: number;
	
	    static createFrom(source: any = {}) {
	        return new TailLazyBatchRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.samples = source["samples"];
	        this.hit3Rate = source["hit3Rate"];
	        this.hit5Rate = source["hit5Rate"];
	        this.avgHigh = source["avgHigh"];
	        this.avgOpen = source["avgOpen"];
	        this.avgClose = source["avgClose"];
	        this.tpWinRate = source["tpWinRate"];
	        this.tpExpectancy = source["tpExpectancy"];
	    }
	}
	export class TailLazyBatchResult {
	    start: string;
	    end: string;
	    rows: TailLazyBatchRow[];
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new TailLazyBatchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.rows = this.convertValues(source["rows"], TailLazyBatchRow);
	        this.warning = source["warning"];
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
	
	
	export class TradeActionEntry {
	    id: number;
	    tradeId: number;
	    stockCode: string;
	    stockName: string;
	    action: string;
	    tradeDate: string;
	    price: number;
	    shares: number;
	    amount: number;
	    afterShares: number;
	    afterCost: number;
	    note: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new TradeActionEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tradeId = source["tradeId"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.action = source["action"];
	        this.tradeDate = source["tradeDate"];
	        this.price = source["price"];
	        this.shares = source["shares"];
	        this.amount = source["amount"];
	        this.afterShares = source["afterShares"];
	        this.afterCost = source["afterCost"];
	        this.note = source["note"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class TradeJournalEntry {
	    id: number;
	    stockCode: string;
	    stockName: string;
	    buyDate: string;
	    buyPrice: number;
	    shares: number;
	    sellDate: string;
	    sellPrice: number;
	    currentPrice?: number;
	    status: string;
	    pnl: number;
	    pnlPct: number;
	    holdDays: number;
	    source: string;
	    note: string;
	    actions?: TradeActionEntry[];
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new TradeJournalEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.buyDate = source["buyDate"];
	        this.buyPrice = source["buyPrice"];
	        this.shares = source["shares"];
	        this.sellDate = source["sellDate"];
	        this.sellPrice = source["sellPrice"];
	        this.currentPrice = source["currentPrice"];
	        this.status = source["status"];
	        this.pnl = source["pnl"];
	        this.pnlPct = source["pnlPct"];
	        this.holdDays = source["holdDays"];
	        this.source = source["source"];
	        this.note = source["note"];
	        this.actions = this.convertValues(source["actions"], TradeActionEntry);
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
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
	export class TradeJournalRequest {
	    id: number;
	    action: string;
	    stockCode: string;
	    stockName: string;
	    buyDate: string;
	    buyPrice: number;
	    shares: number;
	    sellDate: string;
	    sellPrice: number;
	    source: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new TradeJournalRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.action = source["action"];
	        this.stockCode = source["stockCode"];
	        this.stockName = source["stockName"];
	        this.buyDate = source["buyDate"];
	        this.buyPrice = source["buyPrice"];
	        this.shares = source["shares"];
	        this.sellDate = source["sellDate"];
	        this.sellPrice = source["sellPrice"];
	        this.source = source["source"];
	        this.note = source["note"];
	    }
	}
	export class TradePeriodStat {
	    period: string;
	    trades: number;
	    wins: number;
	    winRate: number;
	    totalPnl: number;
	    avgPnlPct: number;
	
	    static createFrom(source: any = {}) {
	        return new TradePeriodStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.period = source["period"];
	        this.trades = source["trades"];
	        this.wins = source["wins"];
	        this.winRate = source["winRate"];
	        this.totalPnl = source["totalPnl"];
	        this.avgPnlPct = source["avgPnlPct"];
	    }
	}
	export class TradeJournalSummary {
	    openCount: number;
	    closedCount: number;
	    wins: number;
	    winRate: number;
	    totalPnl: number;
	    avgPnlPct: number;
	    avgWinPct: number;
	    avgLossPct: number;
	    profitFactor: number;
	    avgHoldDays: number;
	
	    static createFrom(source: any = {}) {
	        return new TradeJournalSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openCount = source["openCount"];
	        this.closedCount = source["closedCount"];
	        this.wins = source["wins"];
	        this.winRate = source["winRate"];
	        this.totalPnl = source["totalPnl"];
	        this.avgPnlPct = source["avgPnlPct"];
	        this.avgWinPct = source["avgWinPct"];
	        this.avgLossPct = source["avgLossPct"];
	        this.profitFactor = source["profitFactor"];
	        this.avgHoldDays = source["avgHoldDays"];
	    }
	}
	export class TradeJournalStats {
	    summary: TradeJournalSummary;
	    byDay: TradePeriodStat[];
	    byWeek: TradePeriodStat[];
	    byMonth: TradePeriodStat[];
	
	    static createFrom(source: any = {}) {
	        return new TradeJournalStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = this.convertValues(source["summary"], TradeJournalSummary);
	        this.byDay = this.convertValues(source["byDay"], TradePeriodStat);
	        this.byWeek = this.convertValues(source["byWeek"], TradePeriodStat);
	        this.byMonth = this.convertValues(source["byMonth"], TradePeriodStat);
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
	
	
	export class WaveCandidate {
	    code: string;
	    name: string;
	    price: number;
	    kongpan: number;
	    ignite: boolean;
	    date: string;
	    score: number;
	    level: string;
	    phase: string;
	    eatFish: boolean;
	    relaxedIgnite: boolean;
	    strictIgnite: boolean;
	    recentIgnite: boolean;
	    mainOpenFish: boolean;
	    timelyTakeProfit: boolean;
	    breakTakeProfit: boolean;
	    strongSignal: boolean;
	    strongCount: number;
	    mainRise: boolean;
	    mainControlStart: boolean;
	    mainControlReduce: boolean;
	    buyState: boolean;
	    trendBull: boolean;
	    energyBull: boolean;
	    midBull: boolean;
	    shortBull: boolean;
	    gz: boolean;
	    reasons: string[];
	    risks: string[];
	
	    static createFrom(source: any = {}) {
	        return new WaveCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.kongpan = source["kongpan"];
	        this.ignite = source["ignite"];
	        this.date = source["date"];
	        this.score = source["score"];
	        this.level = source["level"];
	        this.phase = source["phase"];
	        this.eatFish = source["eatFish"];
	        this.relaxedIgnite = source["relaxedIgnite"];
	        this.strictIgnite = source["strictIgnite"];
	        this.recentIgnite = source["recentIgnite"];
	        this.mainOpenFish = source["mainOpenFish"];
	        this.timelyTakeProfit = source["timelyTakeProfit"];
	        this.breakTakeProfit = source["breakTakeProfit"];
	        this.strongSignal = source["strongSignal"];
	        this.strongCount = source["strongCount"];
	        this.mainRise = source["mainRise"];
	        this.mainControlStart = source["mainControlStart"];
	        this.mainControlReduce = source["mainControlReduce"];
	        this.buyState = source["buyState"];
	        this.trendBull = source["trendBull"];
	        this.energyBull = source["energyBull"];
	        this.midBull = source["midBull"];
	        this.shortBull = source["shortBull"];
	        this.gz = source["gz"];
	        this.reasons = source["reasons"];
	        this.risks = source["risks"];
	    }
	}
	export class WaveScanResult {
	    asOf: string;
	    snapshotAsOf: string;
	    dataSource: string;
	    universeCount: number;
	    scannedCount: number;
	    preheatDays: number;
	    patchedCount: number;
	    recentKCount: number;
	    gatePassed: boolean;
	    gateBypassed: boolean;
	    count: number;
	    items: WaveCandidate[];
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new WaveScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asOf = source["asOf"];
	        this.snapshotAsOf = source["snapshotAsOf"];
	        this.dataSource = source["dataSource"];
	        this.universeCount = source["universeCount"];
	        this.scannedCount = source["scannedCount"];
	        this.preheatDays = source["preheatDays"];
	        this.patchedCount = source["patchedCount"];
	        this.recentKCount = source["recentKCount"];
	        this.gatePassed = source["gatePassed"];
	        this.gateBypassed = source["gateBypassed"];
	        this.count = source["count"];
	        this.items = this.convertValues(source["items"], WaveCandidate);
	        this.message = source["message"];
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

}

export namespace services {
	
	export class ArchiveBar {
	    code: string;
	    tradeDate: string;
	    name: string;
	    open: number;
	    high: number;
	    low: number;
	    close: number;
	    prevClose: number;
	    pctChg: number;
	    volume: number;
	    amount: number;
	    turnover: number;
	    volumeRatio: number;
	    pe: number;
	    peTtm: number;
	    pb: number;
	    ps: number;
	    divYield: number;
	    totalMcap: number;
	    floatMcap: number;
	    adjFactor: number;
	    limitUp: number;
	    limitDown: number;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveBar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.tradeDate = source["tradeDate"];
	        this.name = source["name"];
	        this.open = source["open"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.close = source["close"];
	        this.prevClose = source["prevClose"];
	        this.pctChg = source["pctChg"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	        this.turnover = source["turnover"];
	        this.volumeRatio = source["volumeRatio"];
	        this.pe = source["pe"];
	        this.peTtm = source["peTtm"];
	        this.pb = source["pb"];
	        this.ps = source["ps"];
	        this.divYield = source["divYield"];
	        this.totalMcap = source["totalMcap"];
	        this.floatMcap = source["floatMcap"];
	        this.adjFactor = source["adjFactor"];
	        this.limitUp = source["limitUp"];
	        this.limitDown = source["limitDown"];
	    }
	}
	export class ArchiveCoverage {
	    available: boolean;
	    stocks: number;
	    rows: number;
	    minDate: string;
	    maxDate: string;
	    path: string;
	    sizeMb: number;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveCoverage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.stocks = source["stocks"];
	        this.rows = source["rows"];
	        this.minDate = source["minDate"];
	        this.maxDate = source["maxDate"];
	        this.path = source["path"];
	        this.sizeMb = source["sizeMb"];
	    }
	}
	export class ArchiveStockInfo {
	    code: string;
	    name: string;
	    firstDate: string;
	    lastDate: string;
	    rows: number;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveStockInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.firstDate = source["firstDate"];
	        this.lastDate = source["lastDate"];
	        this.rows = source["rows"];
	    }
	}
	export class AuctionFinalRow {
	    stockCode: string;
	    name: string;
	    price: number;
	    pct: number;
	    volume: number;
	    amount: number;
	    volumeRatio: number;
	    floatMcap: number;
	
	    static createFrom(source: any = {}) {
	        return new AuctionFinalRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stockCode = source["stockCode"];
	        this.name = source["name"];
	        this.price = source["price"];
	        this.pct = source["pct"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	        this.volumeRatio = source["volumeRatio"];
	        this.floatMcap = source["floatMcap"];
	    }
	}
	export class BackupResult {
	    at: string;
	    dest: string;
	    files: string[];
	    totalBytes: number;
	    durationMs: number;
	    warnings: string[];
	    ok: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.dest = source["dest"];
	        this.files = source["files"];
	        this.totalBytes = source["totalBytes"];
	        this.durationMs = source["durationMs"];
	        this.warnings = source["warnings"];
	        this.ok = source["ok"];
	    }
	}
	export class CninfoAnnouncement {
	    title: string;
	    time: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new CninfoAnnouncement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.time = source["time"];
	        this.url = source["url"];
	    }
	}
	export class CninfoResult {
	    code: string;
	    searchKey: string;
	    total: number;
	    announcements: CninfoAnnouncement[];
	
	    static createFrom(source: any = {}) {
	        return new CninfoResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.searchKey = source["searchKey"];
	        this.total = source["total"];
	        this.announcements = this.convertValues(source["announcements"], CninfoAnnouncement);
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
	export class GubaPost {
	    title: string;
	    time: string;
	    clicks: number;
	    comments: number;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new GubaPost(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.time = source["time"];
	        this.clicks = source["clicks"];
	        this.comments = source["comments"];
	        this.url = source["url"];
	    }
	}
	export class GubaSummary {
	    code: string;
	    barName: string;
	    posts: GubaPost[];
	    fetchedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new GubaSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.barName = source["barName"];
	        this.posts = this.convertValues(source["posts"], GubaPost);
	        this.fetchedAt = source["fetchedAt"];
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
	export class IntradayCoverage {
	    auctionDays: number;
	    minuteDays: number;
	    auctionRows: number;
	    finalRows: number;
	    minuteRows: number;
	    focusRows: number;
	    firstDate: string;
	    lastDate: string;
	    lastTickTime: string;
	
	    static createFrom(source: any = {}) {
	        return new IntradayCoverage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auctionDays = source["auctionDays"];
	        this.minuteDays = source["minuteDays"];
	        this.auctionRows = source["auctionRows"];
	        this.finalRows = source["finalRows"];
	        this.minuteRows = source["minuteRows"];
	        this.focusRows = source["focusRows"];
	        this.firstDate = source["firstDate"];
	        this.lastDate = source["lastDate"];
	        this.lastTickTime = source["lastTickTime"];
	    }
	}
	export class IntradayTick {
	    time: string;
	    price: number;
	    pct: number;
	    volume: number;
	    amount: number;
	
	    static createFrom(source: any = {}) {
	        return new IntradayTick(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.price = source["price"];
	        this.pct = source["pct"];
	        this.volume = source["volume"];
	        this.amount = source["amount"];
	    }
	}
	export class LongHuBangListResult {
	    items: models.LongHuBangItem[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new LongHuBangListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], models.LongHuBangItem);
	        this.total = source["total"];
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
	export class MarginDay {
	    date: string;
	    rzye: number;
	    rqye: number;
	    rzrqTotal: number;
	
	    static createFrom(source: any = {}) {
	        return new MarginDay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.rzye = source["rzye"];
	        this.rqye = source["rqye"];
	        this.rzrqTotal = source["rzrqTotal"];
	    }
	}
	export class SectorMove {
	    name: string;
	    changePct: number;
	
	    static createFrom(source: any = {}) {
	        return new SectorMove(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.changePct = source["changePct"];
	    }
	}
	export class MarketMood {
	    date: string;
	    upCount: number;
	    downCount: number;
	    flatCount: number;
	    strongCount: number;
	    weakCount: number;
	    marginTrend: MarginDay[];
	    topSectors: SectorMove[];
	    bottomSectors: SectorMove[];
	    fetchedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new MarketMood(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.upCount = source["upCount"];
	        this.downCount = source["downCount"];
	        this.flatCount = source["flatCount"];
	        this.strongCount = source["strongCount"];
	        this.weakCount = source["weakCount"];
	        this.marginTrend = this.convertValues(source["marginTrend"], MarginDay);
	        this.topSectors = this.convertValues(source["topSectors"], SectorMove);
	        this.bottomSectors = this.convertValues(source["bottomSectors"], SectorMove);
	        this.fetchedAt = source["fetchedAt"];
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
	export class MarketStatus {
	    status: string;
	    statusText: string;
	    isTradeDay: boolean;
	    holidayName: string;
	
	    static createFrom(source: any = {}) {
	        return new MarketStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.isTradeDay = source["isTradeDay"];
	        this.holidayName = source["holidayName"];
	    }
	}
	
	export class StockSearchResult {
	    symbol: string;
	    name: string;
	    industry: string;
	    market: string;
	
	    static createFrom(source: any = {}) {
	        return new StockSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.symbol = source["symbol"];
	        this.name = source["name"];
	        this.industry = source["industry"];
	        this.market = source["market"];
	    }
	}
	export class Telegraph {
	    time: string;
	    content: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Telegraph(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.content = source["content"];
	        this.url = source["url"];
	    }
	}
	export class TradingPeriod {
	    status: string;
	    text: string;
	    startTime: string;
	    endTime: string;
	
	    static createFrom(source: any = {}) {
	        return new TradingPeriod(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.text = source["text"];
	        this.startTime = source["startTime"];
	        this.endTime = source["endTime"];
	    }
	}
	export class TradingSchedule {
	    isTradeDay: boolean;
	    holidayName: string;
	    periods: TradingPeriod[];
	
	    static createFrom(source: any = {}) {
	        return new TradingSchedule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isTradeDay = source["isTradeDay"];
	        this.holidayName = source["holidayName"];
	        this.periods = this.convertValues(source["periods"], TradingPeriod);
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
	export class UpdateInfo {
	    hasUpdate: boolean;
	    latestVersion: string;
	    currentVersion: string;
	    releaseUrl: string;
	    releaseNotes: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasUpdate = source["hasUpdate"];
	        this.latestVersion = source["latestVersion"];
	        this.currentVersion = source["currentVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.releaseNotes = source["releaseNotes"];
	        this.error = source["error"];
	    }
	}

}

export namespace tools {
	
	export class ToolInfo {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}

}

