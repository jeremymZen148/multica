import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const telegramKeys = {
  all: (wsId: string) => ["telegram", wsId] as const,
  integration: (wsId: string) => [...telegramKeys.all(wsId), "integration"] as const,
};

export const telegramIntegrationOptions = (wsId: string) =>
  queryOptions({
    queryKey: telegramKeys.integration(wsId),
    queryFn: () => api.getTelegramIntegration(wsId),
    enabled: !!wsId,
  });
