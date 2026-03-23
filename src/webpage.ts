import * as vscode from 'vscode';
import * as path from 'path';

type RuntimeSocket = {
    readyState: number;
    send: (data: string) => void;
    addEventListener: (type: string, listener: (event: { data: unknown }) => void) => void;
    onclose: (() => void) | null;
};

const SOCKET_OPEN = 1;

export class WebPageProvider implements vscode.WebviewViewProvider {
    private outputChannel: vscode.OutputChannel;
    private socket: RuntimeSocket | undefined;
    private webviewView: vscode.WebviewView | undefined;
    private recentConsoleLines: string[] = [];
    private consoleTailBuffer = '';
    private recentTerminalLines: string[] = [];
    private terminalTailBuffer = '';

    constructor(private readonly _extensionUri: vscode.Uri) {
        this.outputChannel = vscode.window.createOutputChannel('Tcl Console');
        const webSocketCtor = (globalThis as { WebSocket?: new (url: string) => RuntimeSocket }).WebSocket;
        if (typeof webSocketCtor === 'function') {
            this.socket = new webSocketCtor('wss://ai-coder.thucs.cn/api/vivado');
            this.socket.addEventListener('message', (event) => {
                const chunk = String(event.data);
                this.outputChannel.append(chunk);
                this.appendConsoleChunk(chunk);
            });
            this.socket.onclose = () => {
                this.outputChannel.appendLine('\n\nWebSocket连接已断开，请刷新页面');
            };
        } else {
            this.outputChannel.appendLine('当前运行环境不支持 WebSocket，Tcl 通道不可用。');
        }

        // 优先采集 VS Code 终端输出，保证“粘贴最近错误”读取的就是终端最新内容。
        const terminalDataApi = (vscode.window as unknown as {
            onDidWriteTerminalData?: (listener: (e: { data: string }) => void) => vscode.Disposable;
        }).onDidWriteTerminalData;
        if (typeof terminalDataApi === 'function') {
            terminalDataApi((e) => {
                this.appendTerminalChunk(String(e.data || ''));
            });
        }
        this.outputChannel.show();
    }

    async resolveWebviewView(
        webviewView: vscode.WebviewView,
        _: vscode.WebviewViewResolveContext,
        _token: vscode.CancellationToken
    ): Promise<void> {
        this.webviewView = webviewView;
        webviewView.webview.options = {
            enableScripts: true,
            localResourceRoots: [this._extensionUri]
        };
        webviewView.webview.onDidReceiveMessage(async (message: unknown) => {
            if (typeof message === 'string') {
                if (this.socket && this.socket.readyState === SOCKET_OPEN) {
                    this.socket.send(message);
                } else {
                    this.outputChannel.appendLine('Tcl WebSocket 未连接，无法发送指令。');
                }
                return;
            }

            if (!message || typeof message !== 'object') {
                return;
            }

            const payload = message as {
                type?: string;
                path?: string;
                content?: string;
                lines?: number;
            };

            if (payload.type === 'requestRecentConsole') {
                const lines = typeof payload.lines === 'number' ? payload.lines : 120;
                await webviewView.webview.postMessage({
                    type: 'recentConsole',
                    text: this.getRecentConsole(lines)
                });
                return;
            }

            if (payload.type === 'writeFile') {
                if (!payload.path || typeof payload.content !== 'string') {
                    this.outputChannel.appendLine('写文件请求参数缺失，已忽略。');
                    return;
                }
                await this.safeWriteFile(payload.path, payload.content);
            }
        });
        webviewView.webview.html = await this._getHtmlForWebview();
    }

    private appendConsoleChunk(chunk: string): void {
        // Vivado 输出里经常混用 \r/\n；\r 多用于回车刷新，不应直接视为换行。
        // 这里把 \r\n 规整成 \n，并移除其余 \r，避免出现“s\net”这类碎片行。
        const normalized = chunk.replace(/\r\n/g, '\n').replace(/\r/g, '');
        const merged = this.consoleTailBuffer + normalized;
        const lines = merged.split('\n');
        this.consoleTailBuffer = lines.pop() ?? '';

        for (const line of lines) {
            if (line.length === 0) {
                continue;
            }
            this.recentConsoleLines.push(line);
        }
        const maxLines = 2000;
        if (this.recentConsoleLines.length > maxLines) {
            this.recentConsoleLines.splice(0, this.recentConsoleLines.length - maxLines);
        }
    }

    private getRecentConsole(lines: number): string {
        // 若终端数据可用，优先用终端尾部；否则回退到 WebSocket 缓存。
        if (this.recentTerminalLines.length > 0 || this.terminalTailBuffer.trim().length > 0) {
            return this.getRecentTerminal(lines);
        }

        const safeLines = Math.max(1, Math.min(lines, 500));
        const outputLines = this.recentConsoleLines.slice(-safeLines);
        if (this.consoleTailBuffer.trim().length > 0) {
            outputLines.push(this.consoleTailBuffer);
        }
        const joined = outputLines.join('\n');

        // 以 "Vivado%" 为交互段标记，仅返回最近几段，避免一次粘贴过长。
        const marker = 'Vivado%';
        const markerIndexes: number[] = [];
        let searchFrom = 0;
        while (true) {
            const idx = joined.indexOf(marker, searchFrom);
            if (idx === -1) {
                break;
            }
            markerIndexes.push(idx);
            searchFrom = idx + marker.length;
        }

        if (markerIndexes.length === 0) {
            return this.limitConsoleSize(joined);
        }

        // 保留最近 3 段交互，避免整段历史粘贴进输入框。
        const keepMarkers = 3;
        const startIdx = markerIndexes[Math.max(0, markerIndexes.length - keepMarkers)];
        return this.limitConsoleSize(joined.slice(startIdx).trim());
    }

    private appendTerminalChunk(chunk: string): void {
        const normalized = chunk.replace(/\r\n/g, '\n').replace(/\r/g, '');
        const merged = this.terminalTailBuffer + normalized;
        const lines = merged.split('\n');
        this.terminalTailBuffer = lines.pop() ?? '';

        for (const line of lines) {
            if (line.length === 0) {
                continue;
            }
            this.recentTerminalLines.push(line);
        }
        const maxLines = 3000;
        if (this.recentTerminalLines.length > maxLines) {
            this.recentTerminalLines.splice(0, this.recentTerminalLines.length - maxLines);
        }
    }

    private getRecentTerminal(lines: number): string {
        const safeLines = Math.max(1, Math.min(lines, 800));
        const outputLines = this.recentTerminalLines.slice(-safeLines);
        if (this.terminalTailBuffer.trim().length > 0) {
            outputLines.push(this.terminalTailBuffer);
        }
        const joined = outputLines.join('\n');

        const marker = 'Vivado%';
        const markerIndexes: number[] = [];
        let searchFrom = 0;
        while (true) {
            const idx = joined.indexOf(marker, searchFrom);
            if (idx === -1) {
                break;
            }
            markerIndexes.push(idx);
            searchFrom = idx + marker.length;
        }

        if (markerIndexes.length === 0) {
            return this.limitConsoleSize(joined);
        }
        const keepMarkers = 3;
        const startIdx = markerIndexes[Math.max(0, markerIndexes.length - keepMarkers)];
        return this.limitConsoleSize(joined.slice(startIdx).trim());
    }

    private limitConsoleSize(text: string): string {
        const maxChars = 1800;
        if (text.length <= maxChars) {
            return text;
        }
        return text.slice(text.length - maxChars);
    }

    private async safeWriteFile(relativePath: string, content: string): Promise<void> {
        const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
        if (!workspaceFolder) {
            this.outputChannel.appendLine('当前无工作区，无法写入文件。');
            return;
        }

        const normalizedRelative = path.normalize(relativePath).replace(/^(\.\.(\/|\\|$))+/, '');
        if (path.isAbsolute(normalizedRelative) || normalizedRelative.startsWith('..')) {
            this.outputChannel.appendLine(`拒绝写入非法路径: ${relativePath}`);
            return;
        }

        const fileUri = vscode.Uri.joinPath(workspaceFolder.uri, normalizedRelative);
        const parentDir = path.posix.dirname(normalizedRelative.replace(/\\/g, '/'));
        if (parentDir && parentDir !== '.') {
            await vscode.workspace.fs.createDirectory(vscode.Uri.joinPath(workspaceFolder.uri, parentDir));
        }
        await vscode.workspace.fs.writeFile(fileUri, new TextEncoder().encode(content));
        this.outputChannel.appendLine(`已写入文件: ${normalizedRelative}`);
    }

    private async _getHtmlForWebview(): Promise<string> {
        try {
            const htmlUri = vscode.Uri.joinPath(this._extensionUri, 'src', 'view.html');
            const htmlBytes = await vscode.workspace.fs.readFile(htmlUri);
            return new TextDecoder('utf-8').decode(htmlBytes);
        } catch (error) {
            const detail = error instanceof Error ? error.message : String(error);
            this.outputChannel.appendLine(`读取 Webview HTML 失败: ${detail}`);
            return `<!DOCTYPE html><html><body><h3>加载页面失败</h3><p>${detail}</p></body></html>`;
        }
    }
}