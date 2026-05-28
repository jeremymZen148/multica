import { lazy, Suspense } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetail } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

// Lazy-load so an xterm import failure only breaks the terminal panel,
// not the entire issue-detail route (and by extension the whole renderer).
const IssueTerminalPanel = lazy(() =>
  import("@/components/issue-terminal-panel").then((m) => ({
    default: m.IssueTerminalPanel,
  })),
);

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : "Issue");

  if (!id) return null;
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetail
        issueId={id}
        terminalPanel={
          issue ? (
            <ErrorBoundary>
              <Suspense fallback={null}>
                <IssueTerminalPanel
                  key={issue.id}
                  issueId={issue.id}
                  identifier={issue.identifier}
                  wsId={wsId}
                />
              </Suspense>
            </ErrorBoundary>
          ) : undefined
        }
      />
    </ErrorBoundary>
  );
}
