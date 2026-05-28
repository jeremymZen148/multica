import { spawn, type IPty } from "node-pty";
import { platform, homedir } from "os";

const sessions = new Map<string, IPty>();
const replayBuffers = new Map<string, string>();
const REPLAY_MAX_BYTES = 50 * 1024;

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

  const resolvedCwd = cwd.startsWith("~/") || cwd === "~"
    ? homedir() + cwd.slice(1)
    : cwd;

  const pty = spawn(defaultShell(), [], {
    name: "xterm-256color",
    cols,
    rows,
    cwd: resolvedCwd,
    env: { ...(process.env as Record<string, string>) },
  });

  replayBuffers.set(sessionId, "");
  pty.onData((data) => {
    const buf = (replayBuffers.get(sessionId) ?? "") + data;
    replayBuffers.set(sessionId, buf.length > REPLAY_MAX_BYTES ? buf.slice(-REPLAY_MAX_BYTES) : buf);
    onData(data);
  });
  pty.onExit(({ exitCode }) => {
    sessions.delete(sessionId);
    replayBuffers.delete(sessionId);
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

export function getTerminalReplay(sessionId: string): string {
  return replayBuffers.get(sessionId) ?? "";
}

export function killTerminalSession(sessionId: string): void {
  const pty = sessions.get(sessionId);
  if (!pty) return;
  try { pty.kill(); } catch { /* already dead */ }
  sessions.delete(sessionId);
  replayBuffers.delete(sessionId);
}

export function hasTerminalSession(sessionId: string): boolean {
  return sessions.has(sessionId);
}

export function killAllTerminalSessions(): void {
  for (const [id, pty] of sessions) {
    try { pty.kill(); } catch { /* already dead */ }
    sessions.delete(id);
    replayBuffers.delete(id);
  }
}
