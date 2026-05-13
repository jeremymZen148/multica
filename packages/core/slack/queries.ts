import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const slackKeys = {
  all: (wsId: string) => ["slack", wsId] as const,
  integration: (wsId: string) => [...slackKeys.all(wsId), "integration"] as const,
  connectUrl: (wsId: string) => [...slackKeys.all(wsId), "connect-url"] as const,
};

export const slackIntegrationOptions = (wsId: string) =>
  queryOptions({
    queryKey: slackKeys.integration(wsId),
    queryFn: () => api.getSlackIntegration(wsId),
    enabled: !!wsId,
  });

export const slackConnectUrlOptions = (wsId: string) =>
  queryOptions({
    queryKey: slackKeys.connectUrl(wsId),
    queryFn: () => api.getSlackConnectURL(wsId),
    enabled: !!wsId,
  });
