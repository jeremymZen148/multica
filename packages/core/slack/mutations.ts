import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { slackKeys } from "./queries";

export function useDeleteSlackIntegration(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteSlackIntegration(wsId),
    onSuccess: () => qc.invalidateQueries({ queryKey: slackKeys.integration(wsId) }),
  });
}

export function useLinkSlackUser(wsId: string) {
  return useMutation({
    mutationFn: (slackUserId: string) => api.linkSlackUser(wsId, slackUserId),
  });
}

export function useUnlinkSlackUser(wsId: string) {
  return useMutation({
    mutationFn: () => api.unlinkSlackUser(wsId),
  });
}
