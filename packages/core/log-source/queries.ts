import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const logSourceKeys = {
  all: (wsId: string) => ["workspaces", wsId, "log-sources"] as const,
  patterns: (wsId: string) => ["workspaces", wsId, "log-error-patterns"] as const,
};

export const logSourceListOptions = (wsId: string) =>
  queryOptions({
    queryKey: logSourceKeys.all(wsId),
    queryFn: () => api.listLogSources(wsId),
    enabled: !!wsId,
  });

export const logErrorPatternListOptions = (wsId: string, limit?: number, offset?: number) =>
  queryOptions({
    queryKey: [...logSourceKeys.patterns(wsId), { limit, offset }] as const,
    queryFn: () => api.listLogErrorPatterns(wsId, limit, offset),
    enabled: !!wsId,
  });
