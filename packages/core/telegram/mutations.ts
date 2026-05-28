import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { telegramKeys } from "./queries";

export function useUpsertTelegramIntegration(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (botToken: string) => api.upsertTelegramIntegration(wsId, botToken),
    onSuccess: () => qc.invalidateQueries({ queryKey: telegramKeys.integration(wsId) }),
  });
}

export function useDeleteTelegramIntegration(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteTelegramIntegration(wsId),
    onSuccess: () => qc.invalidateQueries({ queryKey: telegramKeys.integration(wsId) }),
  });
}

export function useLinkTelegramUser(wsId: string) {
  return useMutation({
    mutationFn: ({ chatId, username }: { chatId: number; username?: string }) =>
      api.linkTelegramUser(wsId, chatId, username),
  });
}

export function useUnlinkTelegramUser(wsId: string) {
  return useMutation({
    mutationFn: () => api.unlinkTelegramUser(wsId),
  });
}
