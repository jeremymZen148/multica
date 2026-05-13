import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const githubSyncKeys = {
  all: (wsId: string) => ["workspaces", wsId, "github-sync"] as const,
};

export const githubRepoSyncsOptions = (wsId: string) =>
  queryOptions({
    queryKey: githubSyncKeys.all(wsId),
    queryFn: () => api.listGitHubRepoSyncs(wsId),
    enabled: !!wsId,
  });
