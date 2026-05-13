import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const aiProviderKeys = {
  all: (wsId: string) => ["ai-provider", wsId] as const,
  config: (wsId: string) => [...aiProviderKeys.all(wsId), "config"] as const,
};

export const aiProviderConfigOptions = (wsId: string) =>
  queryOptions({
    queryKey: aiProviderKeys.config(wsId),
    queryFn: () => api.getAIProviderConfig(wsId),
    enabled: !!wsId,
  });
