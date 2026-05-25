import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import * as vscode from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const output = vscode.window.createOutputChannel("Kizu Language Server");
  context.subscriptions.push(output);

  context.subscriptions.push(
    vscode.commands.registerCommand("kizu.restartLanguageServer", async () => {
      await restartClient(output);
    })
  );
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async event => {
      if (event.affectsConfiguration("kizu.lsp.path")) {
        await restartClient(output);
      }
    })
  );

  await startClient(output);
}

export async function deactivate(): Promise<void> {
  await stopClient();
}

async function restartClient(output: vscode.OutputChannel): Promise<void> {
  await stopClient();
  await startClient(output);
}

async function startClient(output: vscode.OutputChannel): Promise<void> {
  const command = serverCommand();
  if (!validateExplicitCommand(command)) {
    return;
  }

  const serverOptions: ServerOptions = {
    command,
    args: [],
    options: {}
  };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "kizu" }],
    outputChannel: output,
    synchronize: {
      configurationSection: "kizu"
    }
  };

  client = new LanguageClient(
    "kizu-lsp",
    "Kizu Language Server",
    serverOptions,
    clientOptions
  );
  try {
    await client.start();
  } catch (error) {
    client = undefined;
    const detail = error instanceof Error ? error.message : String(error);
    vscode.window.showErrorMessage(
      `Failed to start kizu-lsp. Install kizu-lsp or set kizu.lsp.path. ${detail}`
    );
  }
}

async function stopClient(): Promise<void> {
  const running = client;
  client = undefined;
  if (running) {
    await running.stop();
  }
}

function serverCommand(): string {
  const configured = vscode.workspace
    .getConfiguration("kizu.lsp")
    .get<string>("path", "")
    .trim();
  if (configured.length === 0) {
    return "kizu-lsp";
  }
  return expandHome(configured);
}

function validateExplicitCommand(command: string): boolean {
  if (command === "kizu-lsp") {
    return true;
  }
  if (fs.existsSync(command)) {
    return true;
  }
  vscode.window.showErrorMessage(
    `kizu.lsp.path does not exist: ${command}`
  );
  return false;
}

function expandHome(input: string): string {
  if (input === "~") {
    return os.homedir();
  }
  if (input.startsWith("~/") || input.startsWith("~" + path.sep)) {
    return path.join(os.homedir(), input.slice(2));
  }
  return input;
}
