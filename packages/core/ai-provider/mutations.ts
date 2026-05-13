import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AIProviderConfigInput } from "../types/ai-provider";
import { aiProviderKeys } from "./queries";

export function useUpsertAIProviderConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: AIProviderConfigInput) => api.upsertAIProviderConfig(wsId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: aiProviderKeys.config(wsId) }),
  });
}

export function useDeleteAIProviderConfig(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteAIProviderConfig(wsId),
    onSuccess: () => qc.invalidateQueries({ queryKey: aiProviderKeys.config(wsId) }),
  });
}
