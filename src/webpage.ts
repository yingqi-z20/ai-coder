import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';

export class WebPageProvider implements vscode.WebviewViewProvider {
    private outputChannel;
    private socket;

    constructor(private readonly _extensionUri: vscode.Uri) {
        this.outputChannel = vscode.window.createOutputChannel('Tcl Console');
        this.socket = new WebSocket('wss://ai-coder.thucs.cn/api/vivado');
        this.socket.addEventListener('message', (event) => {
            this.outputChannel.append(event.data);
        });
        this.socket.onclose =
            () => {
                this.outputChannel.appendLine('\n\nWebSocket连接已断开，请刷新页面');
            };
        this.outputChannel.show();
    }

    resolveWebviewView(
        webviewView: vscode.WebviewView,
        _: vscode.WebviewViewResolveContext,
        _token: vscode.CancellationToken
    ) {
        webviewView.webview.options = {
            enableScripts: true,
            localResourceRoots: [this._extensionUri]
        };
        webviewView.webview.onDidReceiveMessage((message: string) => {
            this.socket.send(message);
        });
        webviewView.webview.html = this._getHtmlForWebview();
    }

    private _getHtmlForWebview(): string {
        const htmlPath = path.join(this._extensionUri.fsPath, 'src', 'view.html');
        const htmlContent = fs.readFileSync(htmlPath, 'utf8');
        return htmlContent;
    }
}