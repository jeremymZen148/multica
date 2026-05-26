import { spawn, type IPty } from "node-pty";
import { platform } from "os";

const sessions = new Map<string, IPty>();

function defaultShell(): string {
  if (platform() === "win32") return "powershell.exe";
  return process.env.SHELL ?? "/bin/zsh";
}

export function createTerminalSession(
  sessionId: string,
  cwd: string,
  cols: number,
  rows: number,
  onData: (data: string) => void,
  onExit: (exitCode: number) => void,
): void {
  const existing = sessions.get(sessionId);
  if (existing) {
    try { existing.kill(); } catch { /* already dead */ }
    sessions.delete(sessionId);
  }

  const pty = spawn(defaultShell(), [], {
    name: "xterm-256color",
    cols,
    rows,
    cwd,
    env: { ...(process.env as Record<string, string>) },
  });

  pty.onData(onData);
  pty.onExit(({ exitCode }) => {
    sessions.delete(sessionId);
    onExit(exitCode ?? 0);
  });

  sessions.set(sessionId, pty);
}

export function writeToTerminalSession(sessionId: string, data: string): void {
  sessions.get(sessionId)?.write(data);
}

export function resizeTerminalSession(sessionId: string, cols: number, rows: number): void {
  sessions.get(sessionId)?.resize(cols, rows);
}

export function killTerminalSession(sessionId: string): void {
  const pty = sessions.get(sessionId);
  if (!pty) return;
  try { pty.kill(); } catch { /* already dead */ }
  sessions.delete(sessionId);
}

export function killAllTerminalSessions(): void {
  for (const [id, pty] of sessions) {
    try { pty.kill(); } catch { /* already dead */ }
    sessions.delete(id);
  }
}
