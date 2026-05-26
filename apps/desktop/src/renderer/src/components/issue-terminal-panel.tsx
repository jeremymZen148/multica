import { useState, useEffect, useCallback } from "react";
import { GitDiff, Plus, RefreshCw, Terminal } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { TerminalEmulator } from "./terminal-emulator";

interface IssueTerminalPanelProps {
  issueId: string;
  identifier: string;
  wsId: string;
}

type ActiveTab = "terminal" | "changes";

export function IssueTerminalPanel({ issueId, identifier, wsId }: IssueTerminalPanelProps) {
  const [activeTab, setActiveTab] = useState<ActiveTab>("terminal");
  const [repoPath, setRepoPath] = useState<string | null>(null);
  const [repoPathInput, setRepoPathInput] = useState("");
  const [showPathConfig, setShowPathConfig] = useState(false);
  const [changesOutput, setChangesOutput] = useState<string>("");
  const [changesLoading, setChangesLoading] = useState(false);

  // Stable session ID — one terminal per issue
  const sessionId = `issue:${issueId}`;


  // Load repo path for this workspace on mount
  useEffect(() => {
    window.terminalAPI.getRepoPath(wsId).then((path) => {
      setRepoPath(path);
      setRepoPathInput(path ?? "");
    });
  }, [wsId]);

  // Subscribe to IPC data and exit events for this session
  const onDataCallback = useCallback(
    (handler: (data: string) => void) => {
      return window.terminalAPI.onData(({ sessionId: sid, data }) => {
        if (sid === sessionId) handler(data);
      });
    },
    [sessionId],
  );

  const handleMount = useCallback((_write: (data: string) => void) => {}, []);

  const handleReady = useCallback(() => {
    if (!repoPath) return;
    const cols = 120;
    const rows = 30;
    window.terminalAPI.create(sessionId, repoPath, cols, rows);
  }, [sessionId, repoPath]);



  // Kill session on unmount
  useEffect(() => {
    return () => {
      window.terminalAPI.kill(sessionId).catch(() => {});
    };
  }, [sessionId]);

  async function saveRepoPath() {
    const trimmed = repoPathInput.trim();
    if (!trimmed) return;
    await window.terminalAPI.setRepoPath(wsId, trimmed);
    setRepoPath(trimmed);
    setShowPathConfig(false);
  }

  async function loadChanges() {
    if (!repoPath) return;
    setChangesLoading(true);
    try {
      // Run git diff via a one-shot terminal session
      const diffSessionId = `diff:${issueId}:${Date.now()}`;
      const outputParts: string[] = [];

      await new Promise<void>((resolve) => {
        const unsub = window.terminalAPI.onData(({ sessionId: sid, data }) => {
          if (sid === diffSessionId) outputParts.push(data);
        });
        const unsubExit = window.terminalAPI.onExit(({ sessionId: sid }) => {
          if (sid === diffSessionId) {
            unsub();
            unsubExit();
            resolve();
          }
        });

        window.terminalAPI.create(diffSessionId, repoPath, 120, 50).then(() => {
          // Run git diff and exit
          window.terminalAPI.write(diffSessionId, "git diff HEAD --stat 2>&1; echo '---DIFF---'; git diff HEAD 2>&1; exit 0\r");
        });

        // Timeout fallback
        setTimeout(() => {
          unsub();
          unsubExit();
          window.terminalAPI.kill(diffSessionId).catch(() => {});
          resolve();
        }, 10000);
      });

      setChangesOutput(outputParts.join(""));
    } finally {
      setChangesLoading(false);
    }
  }

  useEffect(() => {
    if (activeTab === "changes" && repoPath) {
      loadChanges();
    }
  }, [activeTab, repoPath]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!repoPath || showPathConfig) {
    return (
      <div className="flex h-full flex-col">
        <PanelTabBar activeTab={activeTab} onTabChange={setActiveTab} identifier={identifier} />
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="flex w-full max-w-sm flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              Set the local repo path to open a terminal for <span className="font-mono font-medium text-foreground">{identifier}</span>
            </p>
            <div className="flex gap-2">
              <input
                value={repoPathInput}
                onChange={(e) => setRepoPathInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") saveRepoPath(); }}
                placeholder="/Users/you/projects/my-repo"
                className="flex h-8 flex-1 rounded-md border border-input bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
                autoFocus
              />
              <Button size="sm" onClick={saveRepoPath} disabled={!repoPathInput.trim()}>
                Save
              </Button>
              {repoPath && (
                <Button size="sm" variant="ghost" onClick={() => setShowPathConfig(false)}>
                  Cancel
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PanelTabBar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        identifier={identifier}
        onConfigRepo={() => setShowPathConfig(true)}
        onRefreshChanges={activeTab === "changes" ? loadChanges : undefined}
        changesLoading={changesLoading}
      />

      {activeTab === "terminal" && (
        <div className="relative flex-1 overflow-hidden bg-background p-1">
          <TerminalEmulator
            onMount={handleMount}
            onData={onDataCallback}
            onReady={handleReady}
            onResize={(cols, rows) => window.terminalAPI.resize(sessionId, cols, rows)}
            className="h-full"
          />
        </div>
      )}

      {activeTab === "changes" && (
        <ScrollArea className="flex-1">
          <pre className="p-3 font-mono text-xs whitespace-pre-wrap break-words text-foreground">
            {changesOutput
              ? <AnsiText text={changesOutput} />
              : <span className="text-muted-foreground">No changes on this branch</span>
            }
          </pre>
        </ScrollArea>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface PanelTabBarProps {
  activeTab: ActiveTab;
  onTabChange: (tab: ActiveTab) => void;
  identifier: string;
  onConfigRepo?: () => void;
  onRefreshChanges?: () => void;
  changesLoading?: boolean;
}

function PanelTabBar({
  activeTab,
  onTabChange,
  identifier,
  onConfigRepo,
  onRefreshChanges,
  changesLoading,
}: PanelTabBarProps) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-1 border-b bg-muted/40 px-2">
      <button
        onClick={() => onTabChange("terminal")}
        className={cn(
          "flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium transition-colors",
          activeTab === "terminal"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <Terminal className="h-3 w-3" />
        Terminal
      </button>
      <button
        onClick={() => onTabChange("changes")}
        className={cn(
          "flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium transition-colors",
          activeTab === "changes"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <GitDiff className="h-3 w-3" />
        Changes
      </button>

      <span className="ml-auto flex items-center gap-1">
        <span className="font-mono text-xs text-muted-foreground">{identifier}</span>
        {onRefreshChanges && (
          <Button
            size="icon-sm"
            variant="ghost"
            className="h-6 w-6 text-muted-foreground"
            onClick={onRefreshChanges}
            disabled={changesLoading}
          >
            <RefreshCw className={cn("h-3 w-3", changesLoading && "animate-spin")} />
          </Button>
        )}
        {onConfigRepo && (
          <Button
            size="icon-sm"
            variant="ghost"
            className="h-6 w-6 text-muted-foreground"
            onClick={onConfigRepo}
            title="Change repo path"
          >
            <Plus className="h-3 w-3 rotate-45" />
          </Button>
        )}
      </span>
    </div>
  );
}

// Very simple ANSI escape stripper for the changes view (color codes become spans)
function AnsiText({ text }: { text: string }) {
  // Strip control characters that aren't ANSI color codes, keep the text
  const plain = text.replace(/\x1B\[[0-9;]*[mGKHF]/g, "");
  return <>{plain}</>;
}
