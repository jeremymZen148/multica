export interface SlackIntegration {
  id: string;
  workspace_id: string;
  team_id: string;
  team_name: string;
  default_channel_id: string | null;
  default_channel_name: string | null;
  created_at: string;
}

export interface SlackConnectResponse {
  url: string;
  configured: boolean;
}

export interface SlackUserLink {
  user_id: string;
  slack_user_id: string;
}
