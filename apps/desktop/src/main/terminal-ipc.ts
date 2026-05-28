import { ipcMain } from "electron";
import type { BrowserWindow } from "electron";
import { readFile, writeFile, mkdir } from "fs/promises";
import { join } from "path";
import { homedir } from "os";
import {
  createTerminalSession,
  writeToTerminalSession,
  resizeTerminalSession,
  killTerminalSession,
  killAllTerminalSessions,
  hasTerminalSession,
  getTerminalReplay,
} from "./terminal-manager";

const TERMINAL_PREFS_PATH = join(homedir(), ".multica", "terminal_prefs.json");

async function loadTerminalPrefs(): Promise<Record<string, string>> {
  try {
    const raw = await readFile(TERMINAL_PREFS_PATH, "utf-8");
    return JSON.parse(raw) as Record<string, string>;
  } catch {
    return {};
  }
}

async function saveTerminalPrefs(prefs: Record<string, string>): Promise<void> {
  await mkdir(join(homedir(), ".multica"), { recursive: true });
  await writeFile(TERMINAL_PREFS_PATH, JSON.stringify(prefs, null, 2), "utf-8");
}

export function setupTerminal(getMainWindow: () => BrowserWindow | null): void {
  ipcMain.handle(
    "terminal:create",
    (_event, sessionId: string, cwd: string, cols: number, rows: number) => {
      createTerminalSession(
        sessionId,
        cwd,
        cols,
        rows,
        (data) => {
          getMainWindow()?.webContents.send("terminal:data", { sessionId, data });
        },
        (exitCode) => {
          getMainWindow()?.webContents.send("terminal:exit", { sessionId, exitCode });
        },
      );
    },
  );

  ipcMain.on("terminal:write", (_event, sessionId: string, data: string) => {
    writeToTerminalSession(sessionId, data);
  });

  ipcMain.handle("terminal:resize", (_event, sessionId: string, cols: number, rows: number) => {
    resizeTerminalSession(sessionId, cols, rows);
  });

  ipcMain.handle("terminal:kill", (_event, sessionId: string) => {
    killTerminalSession(sessionId);
  });

  ipcMain.handle("terminal:exists", (_event, sessionId: string) => {
    return hasTerminalSession(sessionId);
  });

  ipcMain.handle("terminal:get-replay", (_event, sessionId: string) => {
    return getTerminalReplay(sessionId);
  });

  ipcMain.handle("terminal:get-repo-path", (_event, wsId: string) => {
    return loadTerminalPrefs().then((prefs) => prefs[wsId] ?? null);
  });

  ipcMain.handle("terminal:set-repo-path", (_event, wsId: string, repoPath: string) => {
    return loadTerminalPrefs().then((prefs) => {
      prefs[wsId] = repoPath;
      return saveTerminalPrefs(prefs);
    });
  });
}

export { killAllTerminalSessions };
