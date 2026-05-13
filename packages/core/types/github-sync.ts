export type GitHubSyncDirection = "multica_to_github" | "github_to_multica" | "both";

export interface GitHubRepoSync {
  id: string;
  workspace_id: string;
  installation_id: number;
  repo_owner: string;
  repo_name: string;
  sync_direction: GitHubSyncDirection;
  created_at: string;
}

export interface GitHubRepoSyncInput {
  installation_id: number;
  repo_owner: string;
  repo_name: string;
  sync_direction: GitHubSyncDirection;
}
