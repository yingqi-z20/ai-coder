// The module 'vscode' contains the VS Code extensibility API
// Import the module and reference it with the alias vscode in your code below
import * as vscode from 'vscode';
import {API_KEY, DOMAIN, WebPageProvider} from './webpage';

// This method is called when your extension is activated
// Your extension is activated the very first time the command is executed
export function activate(context: vscode.ExtensionContext) {

    // Use the console to output diagnostic information (console.log) and errors (console.error)
    // This line of code will only be executed once when your extension is activated
    console.log('Congratulations, your extension "ai-coder" is now active!');
    const pwd: string = (() => {
        if (vscode.workspace.workspaceFolders) {
            return vscode.workspace.workspaceFolders[0].uri.fsPath;
        }
        return "";
    })();
    fetch('https://' + DOMAIN + '/api/chat', {
        method: 'POST', headers: {
            'Content-Type': 'application/json', "X-API-Key": API_KEY
        }, body: JSON.stringify({message: "ZU1svmzfSE7zOyk " + pwd})
    }).then(r => {
        console.assert(r);
        const provider = new WebPageProvider(context.extensionUri);
        context.subscriptions.push(vscode.window.registerWebviewViewProvider('webpage', provider));
    });
}

// This method is called when your extension is deactivated
export function deactivate() {
}
