import * as vscode from 'vscode';

type RuntimeSocket = {
    readyState: number;
    send: (data: string) => void;
    addEventListener: (type: string, listener: (event: { data: unknown }) => void) => void;
    onclose: (() => void) | null;
};

const SOCKET_OPEN = 1;

interface Message {
    sender: string;
    text: string;
    type: string;
}

export class WebPageProvider implements vscode.WebviewViewProvider {
    public messageList: Message[] = [];
    private _view?: vscode.WebviewView;
    private outputChannel = vscode.window.createOutputChannel('Tcl Console');
    private readonly vivadoSocket = new WebSocket('wss://ai-coder.thucs.cn/api/vivado');
    private readonly qwenSocket = new WebSocket('wss://ai-coder.thucs.cn/api/qwen');

    constructor(private readonly _extensionUri: vscode.Uri) {
        this.vivadoSocket.addEventListener('message', (event) => {
            this.outputChannel.append(String(event.data));
        });
        this.vivadoSocket.onclose = (event) => {
            this.outputChannel.appendLine('\n\nWebSocket连接已断开，请刷新页面。原因：' + event.code.toString() + event.reason);
        };
        this.qwenSocket.addEventListener('message', (event) => {
            this.messageList.push({sender: "机器人", text: event.data, type: "bot"});
            if (this._view) {
                this._view.webview.postMessage(this.messageList).then(r => {
                    console.assert(r);
                });
            }
        });
        this.qwenSocket.onclose = (event) => {
            this.messageList.push({
                sender: "系统",
                text: "WebSocket连接已断开，请刷新页面。原因：" + event.code.toString() + event.reason,
                type: "system"
            });
            if (this._view) {
                this._view.webview.postMessage(this.messageList).then(r => {
                    console.assert(r);
                });
            }
        };
        this.outputChannel.show();
    }

    async resolveWebviewView(webviewView: vscode.WebviewView, _: vscode.WebviewViewResolveContext, _token: vscode.CancellationToken): Promise<void> {
        webviewView.webview.options = {
            enableScripts: true, localResourceRoots: [this._extensionUri]
        };
        webviewView.webview.onDidReceiveMessage((message: string) => {
            if (this.vivadoSocket && this.vivadoSocket.readyState === SOCKET_OPEN) {
                this.vivadoSocket.send(message);
            } else {
                this.outputChannel.appendLine('Tcl WebSocket 未连接，无法发送指令。');
            }
        });
        this._view = webviewView;
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