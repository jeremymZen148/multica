"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, GitCommitHorizontal, Link2, PanelRight } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import {
  deriveGitHubSettings,
  githubInstallationsOptions,
} from "@multica/core/github";
import { githubRepoSyncsOptions } from "@multica/core/github-sync/queries";
import { useUpsertGitHubRepoSync, useDeleteGitHubRepoSync } from "@multica/core/github-sync/mutations";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import type { GitHubSyncDirection } from "@multica/core/types/github-sync";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { GitHubMark } from "./github-mark";

type SettingsKey =
  | "github_enabled"
  | "github_pr_sidebar_enabled"
  | "co_authored_by_enabled"
  | "github_auto_link_prs_enabled";

export function GitHubTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  // `canView` gates the read-only installation list (every workspace member
  // sees it); `canManage` gates the Connect / Disconnect actions and comes
  // from the backend response (`can_manage`) so the frontend never claims
  // management rights the server would reject. Fall back to local role check
  // for older backends that don't send `can_manage`.
  const canView = !!currentMember;
  const roleBasedCanManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: installationData } = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: !!wsId && canView,
  });
  const installations = installationData?.installations ?? [];
  const configured = installationData?.configured ?? false;
  const canManage =
    installationData?.can_manage !== undefined
      ? installationData.can_manage === true
      : roleBasedCanManage;
  const connected = installations.length > 0;
  const primaryInstallation = installations[0] ?? null;

  const { data: githubRepoSyncs = [] } = useQuery(githubRepoSyncsOptions(wsId));
  const upsertGitHubRepoSync = useUpsertGitHubRepoSync(wsId);
  const deleteGitHubRepoSync = useDeleteGitHubRepoSync(wsId);

  const flags = deriveGitHubSettings(workspace);
  const [savingKey, setSavingKey] = useState<SettingsKey | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  // Issue Sync form state
  const [syncOwner, setSyncOwner] = useState("");
  const [syncName, setSyncName] = useState("");
  const [syncInstallationId, setSyncInstallationId] = useState("");
  const [syncDirection, setSyncDirection] = useState<GitHubSyncDirection>("both");

  async function persistSetting(key: SettingsKey, next: boolean) {
    if (!workspace || savingKey) return;
    setSavingKey(key);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        [key]: next,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_failed));
    } finally {
      setSavingKey(null);
    }
  }

  async function handleConnect() {
    setConnecting(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId);
      if (!resp.configured || !resp.url) {
        toast.error(t(($) => $.github.toast_not_configured));
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_open_failed));
    } finally {
      setConnecting(false);
    }
  }

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteGitHubInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: ["github", wsId] });
      toast.success(t(($) => $.github.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  async function handleSyncAdd() {
    if (!syncOwner.trim() || !syncName.trim() || !syncInstallationId.trim()) return;
    try {
      await upsertGitHubRepoSync.mutateAsync({
        installation_id: parseInt(syncInstallationId, 10),
        repo_owner: syncOwner.trim(),
        repo_name: syncName.trim(),
        sync_direction: syncDirection,
      });
      setSyncOwner("");
      setSyncName("");
      setSyncInstallationId("");
      setSyncDirection("both");
      toast.success(t(($) => $.github.issue_sync_toast_added));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.issue_sync_toast_add_failed));
    }
  }

  async function handleSyncRemove(repoOwner: string, repoName: string) {
    try {
      await deleteGitHubRepoSync.mutateAsync({ repoOwner, repoName });
      toast.success(t(($) => $.github.issue_sync_toast_removed));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.issue_sync_toast_remove_failed));
    }
  }

  if (!workspace) return null;

  const repositoriesHref = `${navigation.pathname}?tab=repositories`;

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.github.page_description)}
        </p>
      </section>

      <section className="space-y-3">
        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <GitHubMark className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="github-master" className="text-sm font-medium">
                    {t(($) => $.github.section_master)}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {flags.enabled
                      ? t(($) => $.github.master_description_on)
                      : t(($) => $.github.master_description_off)}
                  </p>
                </div>
              </div>
              <Switch
                id="github-master"
                checked={flags.enabled}
                onCheckedChange={(v) => persistSetting("github_enabled", v)}
                disabled={!canManage || savingKey === "github_enabled"}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.github.section_connection)}</h2>
        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <GitHubMark className="h-6 w-6 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">{t(($) => $.github.connection_title)}</p>
                  {connected ? (
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.github.connected_to, {
                        login: installations.map((i) => i.account_login).join(", "),
                      })}
                    </p>
                  ) : canManage ? (
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.github.connection_description_prefix)}{" "}
                      <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                        {t(($) => $.github.connection_identifier_example)}
                      </code>{" "}
                      {t(($) => $.github.connection_description_suffix)}{" "}
                      <strong>{t(($) => $.github.connection_description_done)}</strong>.
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.github.contact_admin_to_connect)}
                    </p>
                  )}
                </div>
              </div>
              {canManage && (
                <div className="flex items-center gap-2">
                  {connected && primaryInstallation ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setDisconnectTarget(primaryInstallation.id)}
                    >
                      {t(($) => $.github.disconnect)}
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      onClick={handleConnect}
                      disabled={connecting || !configured}
                      title={
                        !configured
                          ? t(($) => $.github.connect_disabled_tooltip)
                          : undefined
                      }
                    >
                      {connecting
                        ? t(($) => $.github.connect_opening)
                        : t(($) => $.github.connect_github)}
                    </Button>
                  )}
                </div>
              )}
            </div>

            {canManage && !configured && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.github.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_APP_SLUG</code>{" "}
                {t(($) => $.github.not_configured_and)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_WEBHOOK_SECRET</code>.
              </p>
            )}

            {!canManage && connected && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.github.read_only_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.github.section_features)}</h2>
        <Card>
          <CardContent className="space-y-4">
            <FeatureRow
              id="github-pr-sidebar"
              icon={<PanelRight className="h-4 w-4" />}
              label={t(($) => $.github.feature_pr_sidebar_label)}
              description={
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.github.feature_pr_sidebar_description)}
                </p>
              }
              checked={flags.prSidebar}
              disabled={!canManage || !flags.enabled || savingKey === "github_pr_sidebar_enabled"}
              onCheckedChange={(v) => persistSetting("github_pr_sidebar_enabled", v)}
            />

            <FeatureRow
              id="github-coauthor"
              icon={<GitCommitHorizontal className="h-4 w-4" />}
              label={t(($) => $.github.feature_co_author_label)}
              description={
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.github.feature_co_author_description_prefix)}{" "}
                  <code className="rounded bg-muted px-1 py-0.5 text-xs">
                    {"Co-authored-by: multica-agent <github@multica.ai>"}
                  </code>{" "}
                  {t(($) => $.github.feature_co_author_description_suffix)}
                </p>
              }
              checked={flags.coAuthor}
              disabled={!canManage || !flags.enabled || savingKey === "co_authored_by_enabled"}
              onCheckedChange={(v) => persistSetting("co_authored_by_enabled", v)}
            />

            <FeatureRow
              id="github-auto-link"
              icon={<Link2 className="h-4 w-4" />}
              label={t(($) => $.github.feature_auto_link_label)}
              description={
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.github.feature_auto_link_description)}
                </p>
              }
              checked={flags.autoLinkPRs}
              disabled={!canManage || !flags.enabled || savingKey === "github_auto_link_prs_enabled"}
              onCheckedChange={(v) => persistSetting("github_auto_link_prs_enabled", v)}
            />
          </CardContent>
        </Card>
      </section>

      {canManage && (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">{t(($) => $.github.issue_sync_title)}</h2>
          <Card>
            <CardContent className="space-y-4">
              <p className="text-xs text-muted-foreground">
                {t(($) => $.github.issue_sync_description)}
              </p>

              {githubRepoSyncs.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t(($) => $.github.issue_sync_no_repos)}</p>
              ) : (
                <div className="space-y-2">
                  {githubRepoSyncs.map((sync) => (
                    <div
                      key={sync.id}
                      className="flex items-center justify-between gap-2 rounded-md border px-3 py-2"
                    >
                      <div className="space-y-0.5">
                        <p className="text-xs font-medium">
                          {sync.repo_owner}/{sync.repo_name}
                        </p>
                        <p className="text-[10px] text-muted-foreground">
                          {sync.sync_direction === "both"
                            ? t(($) => $.github.issue_sync_direction_both)
                            : sync.sync_direction === "multica_to_github"
                            ? t(($) => $.github.issue_sync_direction_multica_to_github)
                            : t(($) => $.github.issue_sync_direction_github_to_multica)}
                        </p>
                      </div>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleSyncRemove(sync.repo_owner, sync.repo_name)}
                        disabled={deleteGitHubRepoSync.isPending}
                      >
                        {t(($) => $.github.issue_sync_remove)}
                      </Button>
                    </div>
                  ))}
                </div>
              )}

              <div className="space-y-3 border-t pt-4">
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.github.issue_sync_repo_label)}</p>
                  <div className="flex items-center gap-2">
                    <Input
                      className="h-8 text-xs"
                      placeholder={t(($) => $.github.issue_sync_owner_placeholder)}
                      value={syncOwner}
                      onChange={(e) => setSyncOwner(e.target.value)}
                    />
                    <span className="text-xs text-muted-foreground">/</span>
                    <Input
                      className="h-8 text-xs"
                      placeholder={t(($) => $.github.issue_sync_name_placeholder)}
                      value={syncName}
                      onChange={(e) => setSyncName(e.target.value)}
                    />
                  </div>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.github.issue_sync_installation_label)}</p>
                  <Input
                    className="h-8 text-xs"
                    placeholder={t(($) => $.github.issue_sync_installation_placeholder)}
                    value={syncInstallationId}
                    onChange={(e) => setSyncInstallationId(e.target.value)}
                  />
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.github.issue_sync_direction_label)}</p>
                  <Select
                    value={syncDirection}
                    onValueChange={(v) => setSyncDirection(v as GitHubSyncDirection)}
                  >
                    <SelectTrigger className="h-8 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="both">{t(($) => $.github.issue_sync_direction_both)}</SelectItem>
                      <SelectItem value="multica_to_github">{t(($) => $.github.issue_sync_direction_multica_to_github)}</SelectItem>
                      <SelectItem value="github_to_multica">{t(($) => $.github.issue_sync_direction_github_to_multica)}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <Button
                  size="sm"
                  onClick={handleSyncAdd}
                  disabled={
                    !syncOwner.trim() ||
                    !syncName.trim() ||
                    !syncInstallationId.trim() ||
                    upsertGitHubRepoSync.isPending
                  }
                >
                  {upsertGitHubRepoSync.isPending
                    ? t(($) => $.github.issue_sync_adding)
                    : t(($) => $.github.issue_sync_add)}
                </Button>
              </div>
            </CardContent>
          </Card>
        </section>
      )}

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.github.section_repositories)}</h2>
        <Card>
          <CardContent>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-sm font-medium">
                {t(($) => $.github.repositories_shortcut_label)}
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => navigation.push(repositoriesHref)}
              >
                <ExternalLink className="h-3 w-3" />
                {t(($) => $.github.repositories_shortcut_link)}
              </Button>
            </div>
          </CardContent>
        </Card>
      </section>

      <AlertDialog
        open={!!disconnectTarget}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setDisconnectTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.github.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.github.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.github.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.github.disconnecting)
                : t(($) => $.github.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function FeatureRow({
  id,
  icon,
  label,
  description,
  checked,
  disabled,
  onCheckedChange,
}: {
  id: string;
  icon: React.ReactNode;
  label: string;
  description: React.ReactNode;
  checked: boolean;
  disabled: boolean;
  onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="flex items-start gap-3">
        <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">{icon}</div>
        <div className="space-y-1">
          <Label htmlFor={id} className="text-sm font-medium">
            {label}
          </Label>
          {description}
        </div>
      </div>
      <Switch
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={onCheckedChange}
      />
    </div>
  );
}
