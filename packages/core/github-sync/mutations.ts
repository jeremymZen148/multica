import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { githubSyncKeys } from "./queries";
import type { GitHubRepoSyncInput } from "../types/github-sync";

export function useUpsertGitHubRepoSync(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: GitHubRepoSyncInput) => api.upsertGitHubRepoSync(wsId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: githubSyncKeys.all(wsId) }),
  });
}

export function useDeleteGitHubRepoSync(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoOwner, repoName }: { repoOwner: string; repoName: string }) =>
      api.deleteGitHubRepoSync(wsId, repoOwner, repoName),
    onSuccess: () => qc.invalidateQueries({ queryKey: githubSyncKeys.all(wsId) }),
  });
}
