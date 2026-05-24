"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw, ExternalLink, Trash2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { logErrorPatternListOptions, logSourceListOptions } from "@multica/core/log-source/queries";
import {
  useCreateIssueFromPattern,
  useDeleteLogErrorPattern,
  useTriggerLogSourcePoll,
} from "@multica/core/log-source/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../layout/page-header";

function formatRelative(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
  if (diffDays === 0) return "Today";
  if (diffDays === 1) return "Yesterday";
  if (diffDays < 7) return `${diffDays}d ago`;
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`;
  return `${Math.floor(diffDays / 30)}mo ago`;
}

export function LogsPage() {
  const wsId = useWorkspaceId();
  const [refreshingAll, setRefreshingAll] = useState(false);

  const { data: patterns = [], isLoading } = useQuery(logErrorPatternListOptions(wsId));
  const { data: sources = [] } = useQuery(logSourceListOptions(wsId));

  const createIssue = useCreateIssueFromPattern(wsId);
  const deletePattern = useDeleteLogErrorPattern(wsId);
  const triggerPoll = useTriggerLogSourcePoll(wsId);

  const sourceNameMap = Object.fromEntries(sources.map((s) => [s.id, s.name]));

  async function handleRefreshAll() {
    if (sources.length === 0) return;
    setRefreshingAll(true);
    try {
      await Promise.all(sources.map((s) => triggerPoll.mutateAsync(s.id)));
      toast.success("Poll triggered for all sources");
    } catch {
      toast.error("Failed to trigger poll");
    } finally {
      setRefreshingAll(false);
    }
  }

  async function handleCreateIssue(patternId: string) {
    try {
      await createIssue.mutateAsync(patternId);
      toast.success("Issue created");
    } catch {
      toast.error("Failed to create issue");
    }
  }

  async function handleDismiss(patternId: string) {
    try {
      await deletePattern.mutateAsync(patternId);
      toast.success("Pattern dismissed");
    } catch {
      toast.error("Failed to dismiss pattern");
    }
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader>
        <div className="flex flex-1 items-center justify-between">
          <div className="flex items-center gap-2">
            <h1 className="text-sm font-semibold">Log Patterns</h1>
            {!isLoading && (
              <span className="text-xs text-muted-foreground">
                {patterns.length} {patterns.length === 1 ? "pattern" : "patterns"}
              </span>
            )}
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={handleRefreshAll}
            disabled={refreshingAll || sources.length === 0}
          >
            <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${refreshingAll ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-auto p-4">
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-md" />
            ))}
          </div>
        ) : patterns.length === 0 ? (
          <div className="flex h-48 items-center justify-center">
            <p className="text-sm text-muted-foreground">No recurring error patterns detected yet.</p>
          </div>
        ) : (
          <div className="overflow-hidden rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/40">
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Title</th>
                  <th className="px-4 py-2.5 text-right text-xs font-medium text-muted-foreground">Occurrences</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Source</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Status</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">First seen</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Last seen</th>
                  <th className="px-4 py-2.5 text-right text-xs font-medium text-muted-foreground">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {patterns.map((pattern) => {
                  const sourceName = sourceNameMap[pattern.log_source_id] ?? pattern.log_source_id.slice(0, 8);
                  const hasIssue = !!pattern.issue_id;
                  return (
                    <tr key={pattern.id} className="hover:bg-muted/30">
                      <td className="max-w-xs px-4 py-3">
                        <p className="truncate text-xs font-medium">{pattern.title}</p>
                        <p className="mt-0.5 truncate text-[10px] font-mono text-muted-foreground">
                          {pattern.sample}
                        </p>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <span className="text-xs font-semibold tabular-nums">
                          {pattern.occurrence_count.toLocaleString()}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <span className="max-w-[120px] truncate text-xs text-muted-foreground">{sourceName}</span>
                      </td>
                      <td className="px-4 py-3">
                        {hasIssue ? (
                          <Badge variant="secondary" className="text-[10px]">
                            <ExternalLink className="mr-1 h-3 w-3" />
                            Issue created
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-[10px]">
                            No issue
                          </Badge>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-xs text-muted-foreground">{formatRelative(pattern.first_seen_at)}</span>
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-xs text-muted-foreground">{formatRelative(pattern.last_seen_at)}</span>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-7 text-xs"
                            disabled={hasIssue || createIssue.isPending}
                            onClick={() => handleCreateIssue(pattern.id)}
                          >
                            {hasIssue ? "Issue created" : "Create issue"}
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
                            disabled={deletePattern.isPending}
                            onClick={() => handleDismiss(pattern.id)}
                            title="Dismiss"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
