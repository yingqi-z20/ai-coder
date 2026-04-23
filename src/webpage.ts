import * as vscode from 'vscode';
import * as path from 'path';
import {WebSocket} from "node:http";
import {env} from "node:process";

export const DOMAIN = env.DOMAIN || 'ai-coder.thucs.cn';
export const API_KEY = env.API_KEY || '';
const SOCKET_OPEN = 1;

interface Message {
    sender: string;
    text: string;
    type: string;
}

export class WebPageProvider implements vscode.WebviewViewProvider {
    public messageList: Message[] = [];
    private _view?: vscode.WebviewView;
    private tclConsole = vscode.window.createOutputChannel('Tcl Console');
    private tclConsoleData = "";
    private readonly vivadoSocket = new WebSocket('wss://' + DOMAIN + '/api/vivado?api_key=' + API_KEY);
    private readonly qwenSocket = new WebSocket('wss://' + DOMAIN + '/api/qwen?api_key=' + API_KEY);
    private messageQueue: Promise<void> = Promise.resolve();

    constructor(private readonly _extensionUri: vscode.Uri) {
        this.vivadoSocket.addEventListener('message', (event) => {
            this.tclConsole.append(String(event.data));
            this.tclConsoleData += String(event.data);
        });
        this.vivadoSocket.onclose = (event) => {
            this.tclConsole.appendLine('\n\nTcl Console WebSocket 连接已断开，请刷新页面。原因：' + event.code.toString() + event.reason);
        };
        this.qwenSocket.addEventListener('message', (event) => {
            if (event.data === "<ZU1svmzfSE7zOyk>") {
                this.messageList.push({sender: "机器人", text: "", type: "bot"});
                this.syncLastMessage();
            } else if (event.data === "</ZU1svmzfSE7zOyk>") {
                this.messageList[this.messageList.length - 1].type = "replace";
                this.syncLastMessage();
            } else {
                this.messageList.push({sender: "机器人", text: event.data, type: "append"});
                this.syncLastMessage();
                this.messageList.pop();
                this.messageList[this.messageList.length - 1].text += event.data;
            }
        });
        this.qwenSocket.onclose = (event) => {
            this.messageList.push({
                sender: "系统",
                text: "Agent WebSocket 连接已断开，请刷新页面。原因：" + event.code.toString() + event.reason,
                type: "system"
            });
            this.syncLastMessage();
        };
        /*
        // 优先采集 VS Code 终端输出，保证“粘贴最近错误”读取的就是终端最新内容。
        const terminalDataApi = (vscode.window as unknown as {
            onDidWriteTerminalData?: (listener: (e: { data: string }) => void) => vscode.Disposable;
        }).onDidWriteTerminalData;
        if (typeof terminalDataApi === 'function') {
            terminalDataApi((e) => {
                this.appendTerminalChunk(String(e.data || ''));
            });
        }
        */
        this.tclConsole.show();
    }

    async resolveWebviewView(webviewView: vscode.WebviewView, _: vscode.WebviewViewResolveContext, _token: vscode.CancellationToken): Promise<void> {
        webviewView.webview.options = {
            enableScripts: true, localResourceRoots: [this._extensionUri]
        };
        this._view = webviewView;
        webviewView.webview.onDidReceiveMessage((message) => {
            this.messageQueue = this.messageQueue.then(() => this.messageHandler(message));
        });
        webviewView.webview.html = await this._getHtmlForWebview();
    }

    private async messageHandler(message: unknown) {
        if (typeof message === 'string') {
            const cmd = this.normalizeVivadoCommand(message);
            if (!cmd) {
                return;
            }
            if (this.vivadoSocket && this.vivadoSocket.readyState === SOCKET_OPEN) {
                this.vivadoSocket.send(cmd + '\n');
            } else {
                this.tclConsole.appendLine('Tcl Console WebSocket 未连接，无法发送指令。');
            }
            return;
        }

        if (!message || typeof message !== 'object') {
            return;
        }

        const payload = message as {
            type?: string; path?: string; content?: string; lines?: number; url?: string;
        };

        if (payload.type === 'requestRecentConsole') {
            const n = typeof payload.lines === 'number' ? payload.lines : 120;
            const lines = this.tclConsoleData.split('\n');
            const start = lines.length - n > 0 ? lines.length - n : 0;
            if (!this._view) {
                return;
            }
            await this._view.webview.postMessage({
                type: 'recentConsole', text: lines.slice(start, lines.length).join('\n'),
            });
            return;
        }

        if (payload.type === 'openFolder') {
            if (!payload.path) {
                return;
            }
            const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
            if (!workspaceFolder) {
                this.messageList.push({
                    sender: "系统", text: '当前无工作区，无法创建项目。', type: "system"
                });
                this.syncLastMessage();
                return;
            }
            const path = vscode.Uri.joinPath(workspaceFolder.uri, payload.path);
            await vscode.commands.executeCommand('vscode.openFolder', path);
            return;
        }

        if (payload.type === 'openExternal') {
            if (!payload.url) {
                return;
            }
            try {
                void vscode.env.openExternal(vscode.Uri.parse(payload.url));
            } catch (_) {
                this.messageList.push({
                    sender: "系统", text: '打开外部链接失败，请手动复制链接访问。', type: "system"
                });
                this.syncLastMessage();
            }
            return;
        }

        if (payload.type === 'WRITE') {
            if (!payload.path || typeof payload.content !== 'string') {
                this.messageList.push({
                    sender: "系统", text: '写文件请求参数缺失，已忽略。', type: "system"
                });
                this.syncLastMessage();
                return;
            }
            await this.safeWriteFile(payload.path, payload.content, false);
        }

        if (payload.type === 'APPEND') {
            if (!payload.path || typeof payload.content !== 'string') {
                this.messageList.push({
                    sender: "系统", text: '写文件请求参数缺失，已忽略。', type: "system"
                });
                this.syncLastMessage();
                return;
            }
            await this.safeWriteFile(payload.path, payload.content, true);
        }
    }

    private normalizeVivadoCommand(raw: string): string {
        return raw
            .replace(/\r\n/g, '\n')
            .replace(/\r/g, '\n')
            .replace(/[\u0000-\u0008\u000B-\u001F\u007F]/g, '')
            .trim();
    }

    private syncLastMessage() {
        if (this._view) {
            this._view.webview.postMessage(this.messageList).then(r => {
                console.assert(r);
            });
        }
    }

    private async safeWriteFile(relativePath: string, content: string, append: boolean): Promise<void> {
        const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
        if (!workspaceFolder) {
            this.messageList.push({
                sender: "系统", text: '当前无工作区，无法写入文件。', type: "system"
            });
            this.syncLastMessage();
            return;
        }

        const normalizedRelative = path.normalize(relativePath).replace(/^(\.\.(\/|\\|$))+/, '');
        if (path.isAbsolute(normalizedRelative) || normalizedRelative.startsWith('..')) {
            this.messageList.push({
                sender: "系统", text: `拒绝写入非法路径: ${relativePath}`, type: "system"
            });
            this.syncLastMessage();
            return;
        }

        const fileUri = vscode.Uri.joinPath(workspaceFolder.uri, normalizedRelative);
        const parentDir = path.posix.dirname(normalizedRelative.replace(/\\/g, '/'));
        if (parentDir && parentDir !== '.') {
            await vscode.workspace.fs.createDirectory(vscode.Uri.joinPath(workspaceFolder.uri, parentDir));
        }
        if (append) {
            const pre = await vscode.workspace.fs.readFile(fileUri);
            await vscode.workspace.fs.writeFile(fileUri, new TextEncoder().encode(pre.toString() + content));
        } else {
            await vscode.workspace.fs.writeFile(fileUri, new TextEncoder().encode(content));
        }
        this.messageList.push({
            sender: "系统", text: `已写入文件: ${normalizedRelative}`, type: "system"
        });
        this.syncLastMessage();
    }

    private async _getHtmlForWebview(): Promise<string> {
        try {
            const htmlUri = vscode.Uri.joinPath(this._extensionUri, 'src', 'view.html');
            const htmlBytes = await vscode.workspace.fs.readFile(htmlUri);
            const html = new TextDecoder('utf-8').decode(htmlBytes);
            return html.replace(/__DOMAIN__/g, DOMAIN).replace(/__API_KEY__/g, API_KEY);
        } catch (error) {
            const detail = error instanceof Error ? error.message : String(error);
            console.log(`读取 Webview HTML 失败: ${detail}`);
            return `<!DOCTYPE html><html lang="zh-CN"><body><h3>加载页面失败</h3><p>${detail}</p></body></html>`;
        }
    }
}