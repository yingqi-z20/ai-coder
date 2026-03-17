import * as vscode from 'vscode';

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

    constructor(private readonly _extensionUri: vscode.Uri) {
        this.outputChannel = vscode.window.createOutputChannel('Tcl Console');
        const webSocketCtor = (globalThis as { WebSocket?: new (url: string) => RuntimeSocket }).WebSocket;
        if (typeof webSocketCtor === 'function') {
            this.socket = new webSocketCtor('wss://ai-coder.thucs.cn/api/vivado');
            this.socket.addEventListener('message', (event) => {
                this.outputChannel.append(String(event.data));
            });
            this.socket.onclose = () => {
                this.outputChannel.appendLine('\n\nWebSocket连接已断开，请刷新页面');
            };
        } else {
            this.outputChannel.appendLine('当前运行环境不支持 WebSocket，Tcl 通道不可用。');
        }
        this.outputChannel.show();
    }

    async resolveWebviewView(
        webviewView: vscode.WebviewView,
        _: vscode.WebviewViewResolveContext,
        _token: vscode.CancellationToken
    ): Promise<void> {
        webviewView.webview.options = {
            enableScripts: true,
            localResourceRoots: [this._extensionUri]
        };
        webviewView.webview.onDidReceiveMessage((message: string) => {
            if (this.socket && this.socket.readyState === SOCKET_OPEN) {
                this.socket.send(message);
            } else {
                this.outputChannel.appendLine('Tcl WebSocket 未连接，无法发送指令。');
            }
        });
        webviewView.webview.html = await this._getHtmlForWebview();
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