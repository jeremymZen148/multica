export type LogProvider = "cloudwatch" | "s3";

export interface LogSource {
  id: string;
  workspace_id: string;
  name: string;
  provider: LogProvider;
  config: Record<string, unknown>;
  enabled: boolean;
  poll_interval_minutes: number;
  auto_create_issues: boolean;
  last_polled_at?: string;
  created_at: string;
}

export interface LogErrorPattern {
  id: string;
  workspace_id: string;
  log_source_id: string;
  fingerprint: string;
  title: string;
  sample: string;
  occurrence_count: number;
  first_seen_at: string;
  last_seen_at: string;
  issue_id?: string;
}

export interface CreateLogSourceInput {
  name: string;
  provider: LogProvider;
  config: Record<string, unknown>;
  enabled?: boolean;
  poll_interval_minutes?: number;
  auto_create_issues?: boolean;
}

export interface UpdateLogSourceInput {
  name?: string;
  config?: Record<string, unknown>;
  enabled?: boolean;
  poll_interval_minutes?: number;
  auto_create_issues?: boolean;
}
