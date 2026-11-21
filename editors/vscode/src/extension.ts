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
const terminalName = "Kizu";
export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const output = vscode.window.createOutputChannel("Kizu Language Server");
  context.subscriptions.push(output);
  context.subscriptions.push(
    vscode.commands.registerCommand("kizu.showLocation", async (uri: string, line: number, character: number) => {
      const position = new vscode.Position(line, character);
      await vscode.window.showTextDocument(vscode.Uri.parse(uri), {
        selection: new vscode.Range(position, position)
      });
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("kizu.restartLanguageServer", async () => {
      await restartClient(output);
    })
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("kizu.runFile", async () => {
      await runCurrentFile("run");
    })
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("kizu.testFile", async () => {
      await runCurrentFile("test");
    })
  );
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async event => {
      if (event.affectsConfiguration("kizu.cli.path")) {
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
  const command = cliCommand();
  if (!validateCLICommand(command)) {
    return;
  }

  const serverOptions: ServerOptions = {
    command,
    args: ["lsp"],
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
    "kizu",
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
      `Failed to start kizu lsp. Install kizu or set kizu.cli.path. ${detail}`
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

async function runCurrentFile(mode: "run" | "test"): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    vscode.window.showErrorMessage("Open a Kizu file before running a Kizu command.");
    return;
  }
  const document = editor.document;
  if (document.uri.scheme !== "file") {
    vscode.window.showErrorMessage("Save this Kizu file before running it.");
    return;
  }
  if (document.languageId !== "kizu" && path.extname(document.fileName) !== ".kizu") {
    vscode.window.showErrorMessage("The active editor is not a Kizu file.");
    return;
  }
  if (document.isDirty) {
    const saved = await document.save();
    if (!saved) {
      vscode.window.showErrorMessage("Save this Kizu file before running it.");
      return;
    }
  }

  const command = cliCommand();
  if (!validateCLICommand(command)) {
    return;
  }

  const terminal = vscode.window.terminals.find(item => item.name === terminalName) ??
    vscode.window.createTerminal(terminalName);
  terminal.show();
  terminal.sendText(`${shellQuote(command)} ${mode} ${shellQuote(document.fileName)}`);
}

function cliCommand(): string {
  const configured = vscode.workspace
    .getConfiguration("kizu.cli")
    .get<string>("path", "kizu")
    .trim();
  if (configured.length === 0) {
    return "kizu";
  }
  return expandHome(configured);
}

function validateCLICommand(command: string): boolean {
  if (!looksLikePath(command)) {
    return true;
  }
  if (fs.existsSync(command)) {
    return true;
  }
  vscode.window.showErrorMessage(`kizu.cli.path does not exist: ${command}`);
  return false;
}

function looksLikePath(command: string): boolean {
  return path.isAbsolute(command) ||
    command.includes("/") ||
    command.includes("\\");
}

function shellQuote(value: string): string {
  if (process.platform === "win32") {
    return `"${value.replace(/"/g, '\\"')}"`;
  }
  return `'${value.replace(/'/g, "'\\''")}'`;
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
