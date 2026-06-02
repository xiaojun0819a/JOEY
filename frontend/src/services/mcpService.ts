import { models } from '../../wailsjs/go/models';
import { GetMCPServers, AddMCPServer, UpdateMCPServer, DeleteMCPServer, GetMCPStatus, TestMCPConnection, GetMCPServerTools } from '../../wailsjs/go/main/App';
import { isWailsGoReady, warnWailsUnavailable } from '../utils/wailsEnv';

export type MCPServerConfig = models.MCPServerConfig;

// MCP 服务器状态
export interface MCPServerStatus {
  id: string;
  connected: boolean;
  error: string;
}

// MCP 工具信息
export interface MCPToolInfo {
  name: string;
  description: string;
  serverId: string;
  serverName: string;
}

export async function getMCPServers(): Promise<MCPServerConfig[]> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('读取MCP服务', 'go');
    return [];
  }
  return await GetMCPServers();
}

export async function addMCPServer(server: MCPServerConfig): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('新增MCP服务', 'go');
    return 'browser-mode:no-op';
  }
  return await AddMCPServer(server as any);
}

export async function updateMCPServer(server: MCPServerConfig): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('更新MCP服务', 'go');
    return 'browser-mode:no-op';
  }
  return await UpdateMCPServer(server as any);
}

export async function deleteMCPServer(id: string): Promise<string> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('删除MCP服务', 'go');
    return 'browser-mode:no-op';
  }
  return await DeleteMCPServer(id);
}

// 获取所有 MCP 服务器状态
export async function getMCPStatus(): Promise<MCPServerStatus[]> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('MCP状态', 'go');
    return [];
  }
  return await GetMCPStatus();
}

// 测试指定 MCP 服务器连接
export async function testMCPConnection(serverID: string): Promise<MCPServerStatus> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('测试MCP连接', 'go');
    return { id: serverID, connected: false, error: '浏览器预览模式暂不支持MCP测试' };
  }
  return await TestMCPConnection(serverID);
}

// 获取指定 MCP 服务器的工具列表
export async function getMCPServerTools(serverID: string): Promise<MCPToolInfo[]> {
  if (!isWailsGoReady()) {
    warnWailsUnavailable('MCP工具列表', 'go');
    return [];
  }
  return await GetMCPServerTools(serverID);
}
