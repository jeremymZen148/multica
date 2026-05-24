import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { logSourceKeys } from "./queries";
import type { CreateLogSourceInput, UpdateLogSourceInput } from "../types/log-source";

export function useCreateLogSource(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateLogSourceInput) => api.createLogSource(wsId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: logSourceKeys.all(wsId) }),
  });
}

export function useUpdateLogSource(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateLogSourceInput }) =>
      api.updateLogSource(wsId, id, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: logSourceKeys.all(wsId) }),
  });
}

export function useDeleteLogSource(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteLogSource(wsId, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: logSourceKeys.all(wsId) }),
  });
}

export function useTriggerLogSourcePoll(wsId: string) {
  return useMutation({
    mutationFn: (id: string) => api.triggerLogSourcePoll(wsId, id),
  });
}

export function useCreateIssueFromPattern(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patternId: string) => api.createIssueFromPattern(wsId, patternId),
    onSuccess: () => qc.invalidateQueries({ queryKey: logSourceKeys.patterns(wsId) }),
  });
}

export function useDeleteLogErrorPattern(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patternId: string) => api.deleteLogErrorPattern(wsId, patternId),
    onSuccess: () => qc.invalidateQueries({ queryKey: logSourceKeys.patterns(wsId) }),
  });
}
