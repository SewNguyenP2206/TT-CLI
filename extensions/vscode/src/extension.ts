import * as vscode from "vscode";
import { execFile } from "node:child_process";

const throttleMs = 60_000;

export function activate(context: vscode.ExtensionContext): void {
  let lastBeat = 0;
  let windowFocused = vscode.window.state.focused;

  const sendBeat = (language = ""): void => {
    if (!windowFocused || Date.now() - lastBeat < throttleMs) return;
    lastBeat = Date.now();
    execFile("tt", ["beat", "--source", "vscode", "--language", language], {
      windowsHide: true,
      timeout: 10_000,
    }, () => undefined);
  };

  context.subscriptions.push(
    vscode.workspace.onDidChangeTextDocument((event) => sendBeat(event.document.languageId)),
    vscode.workspace.onDidSaveTextDocument((document) => sendBeat(document.languageId)),
    vscode.window.onDidChangeActiveTextEditor((editor) => sendBeat(editor?.document.languageId ?? "")),
    vscode.window.onDidChangeWindowState((state) => {
      windowFocused = state.focused;
      if (windowFocused) sendBeat(vscode.window.activeTextEditor?.document.languageId ?? "");
    }),
  );
}

export function deactivate(): void {}