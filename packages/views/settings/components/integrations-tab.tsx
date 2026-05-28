"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { slackIntegrationOptions, slackConnectUrlOptions } from "@multica/core/slack/queries";
import { telegramIntegrationOptions } from "@multica/core/telegram/queries";
import {
  useDeleteSlackIntegration,
  useLinkSlackUser,
  useUnlinkSlackUser,
} from "@multica/core/slack/mutations";
import {
  useUpsertTelegramIntegration,
  useDeleteTelegramIntegration,
  useLinkTelegramUser,
  useUnlinkTelegramUser,
} from "@multica/core/telegram/mutations";
import { aiProviderConfigOptions } from "@multica/core/ai-provider/queries";
import { useUpsertAIProviderConfig, useDeleteAIProviderConfig } from "@multica/core/ai-provider/mutations";
import { useT } from "../../i18n";

function SlackMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z" />
    </svg>
  );
}

function TelegramMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z" />
    </svg>
  );
}

function BotMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} aria-hidden="true" className={className}>
      <path strokeLinecap="round" strokeLinejoin="round" d="M9 3h6M12 3v4M5 7h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2V9a2 2 0 012-2z" />
      <circle cx="9" cy="14" r="1.5" />
      <circle cx="15" cy="14" r="1.5" />
    </svg>
  );
}

// GitHub connection and Issue Sync have moved to the dedicated GitHub Settings tab.
export function IntegrationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const [connectingSlack, setConnectingSlack] = useState(false);
  const [slackUserIdInput, setSlackUserIdInput] = useState("");
  const [slackLinked, setSlackLinked] = useState(false);
  const [telegramTokenInput, setTelegramTokenInput] = useState("");
  const [telegramChatIdInput, setTelegramChatIdInput] = useState("");
  const [telegramLinked, setTelegramLinked] = useState(false);
  const [aiProvider, setAiProvider] = useState("anthropic");
  const [aiModel, setAiModel] = useState("");
  const [aiKey, setAiKey] = useState("");

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  // Slack
  const { data: slackIntegration } = useQuery({
    ...slackIntegrationOptions(wsId),
    enabled: !!wsId,
  });
  const { data: slackConnectData } = useQuery({
    ...slackConnectUrlOptions(wsId),
    enabled: !!wsId && canManage && !slackIntegration,
  });
  const deleteSlack = useDeleteSlackIntegration(wsId);
  const linkSlack = useLinkSlackUser(wsId);
  const unlinkSlack = useUnlinkSlackUser(wsId);

  // Telegram
  const { data: telegramIntegration } = useQuery({
    ...telegramIntegrationOptions(wsId),
    enabled: !!wsId,
  });
  const upsertTelegram = useUpsertTelegramIntegration(wsId);
  const deleteTelegram = useDeleteTelegramIntegration(wsId);
  const linkTelegram = useLinkTelegramUser(wsId);
  const unlinkTelegram = useUnlinkTelegramUser(wsId);

  // AI Provider
  const { data: aiProviderData } = useQuery({
    ...aiProviderConfigOptions(wsId),
    enabled: !!wsId,
  });
  const upsertAIProvider = useUpsertAIProviderConfig(wsId);
  const deleteAIProvider = useDeleteAIProviderConfig(wsId);

  async function handleSlackConnect() {
    if (!slackConnectData?.url) {
      toast.error(t(($) => $.integrations.slack_not_configured));
      return;
    }
    setConnectingSlack(true);
    try {
      window.open(slackConnectData.url, "_blank", "noopener");
    } finally {
      setConnectingSlack(false);
    }
  }

  async function handleSlackDisconnect() {
    try {
      await deleteSlack.mutateAsync();
      toast.success(t(($) => $.integrations.slack_disconnected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_disconnect_failed));
    }
  }

  async function handleSlackLink() {
    const id = slackUserIdInput.trim();
    if (!id) return;
    try {
      await linkSlack.mutateAsync(id);
      setSlackLinked(true);
      setSlackUserIdInput("");
      toast.success(t(($) => $.integrations.slack_account_linked));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_link_failed));
    }
  }

  async function handleSlackUnlink() {
    try {
      await unlinkSlack.mutateAsync();
      setSlackLinked(false);
      toast.success(t(($) => $.integrations.slack_account_unlinked));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_unlink_failed));
    }
  }

  async function handleTelegramSave() {
    const token = telegramTokenInput.trim();
    if (!token) return;
    try {
      await upsertTelegram.mutateAsync(token);
      setTelegramTokenInput("");
      toast.success(t(($) => $.integrations.telegram_connected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_connect_failed));
    }
  }

  async function handleTelegramDisconnect() {
    try {
      await deleteTelegram.mutateAsync();
      toast.success(t(($) => $.integrations.telegram_disconnected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_disconnect_failed));
    }
  }

  async function handleTelegramLink() {
    const chatId = parseInt(telegramChatIdInput.trim(), 10);
    if (isNaN(chatId)) return;
    try {
      await linkTelegram.mutateAsync({ chatId });
      setTelegramLinked(true);
      setTelegramChatIdInput("");
      toast.success(t(($) => $.integrations.telegram_account_linked));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_link_failed));
    }
  }

  async function handleTelegramUnlink() {
    try {
      await unlinkTelegram.mutateAsync();
      setTelegramLinked(false);
      toast.success(t(($) => $.integrations.telegram_account_unlinked));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_unlink_failed));
    }
  }

  async function handleAIProviderSave() {
    try {
      const provider = aiProvider as "anthropic" | "openai" | "gemini";
      await upsertAIProvider.mutateAsync({ provider, model: aiModel, api_key: aiKey });
      setAiKey("");
      toast.success(t(($) => $.integrations.ai_provider_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_connect_failed));
    }
  }

  async function handleAIProviderRemove() {
    try {
      await deleteAIProvider.mutateAsync();
      setAiProvider("anthropic");
      setAiModel("");
      setAiKey("");
      toast.success(t(($) => $.integrations.ai_provider_removed));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_disconnect_failed));
    }
  }

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.section_title)}</h2>

        {/* Slack */}
        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <SlackMark className="h-5 w-5 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">{t(($) => $.integrations.slack_title)}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.slack_description)}
                  </p>
                  {slackIntegration && (
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.integrations.slack_connected_as)}{" "}
                      <span className="font-medium">{slackIntegration.team_name}</span>
                    </p>
                  )}
                </div>
              </div>
              {canManage && (
                slackIntegration ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleSlackDisconnect}
                    disabled={deleteSlack.isPending}
                  >
                    {deleteSlack.isPending
                      ? t(($) => $.integrations.disconnecting)
                      : t(($) => $.integrations.disconnect)}
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={handleSlackConnect}
                    disabled={connectingSlack || slackConnectData?.configured === false}
                    title={slackConnectData?.configured === false
                      ? t(($) => $.integrations.slack_not_configured)
                      : undefined}
                  >
                    {connectingSlack
                      ? t(($) => $.integrations.connect_opening)
                      : t(($) => $.integrations.connect_slack)}
                  </Button>
                )
              )}
            </div>

            {/* Member account linking */}
            <div className="border-t pt-4 space-y-2">
              <p className="text-xs font-medium">{t(($) => $.integrations.personal_link_title)}</p>
              {slackLinked ? (
                <div className="flex items-center gap-2">
                  <p className="text-xs text-muted-foreground">{t(($) => $.integrations.slack_you_are_linked)}</p>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={handleSlackUnlink}
                    disabled={unlinkSlack.isPending}
                  >
                    {t(($) => $.integrations.unlink)}
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <Input
                    className="h-8 text-xs"
                    placeholder={t(($) => $.integrations.slack_user_id_placeholder)}
                    value={slackUserIdInput}
                    onChange={(e) => setSlackUserIdInput(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleSlackLink()}
                  />
                  <Button
                    size="sm"
                    onClick={handleSlackLink}
                    disabled={!slackUserIdInput.trim() || linkSlack.isPending}
                  >
                    {linkSlack.isPending
                      ? t(($) => $.integrations.linking)
                      : t(($) => $.integrations.link_account)}
                  </Button>
                </div>
              )}
              <p className="text-xs text-muted-foreground">{t(($) => $.integrations.slack_user_id_hint)}</p>
            </div>

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>

        {/* Telegram */}
        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <TelegramMark className="h-5 w-5 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">{t(($) => $.integrations.telegram_title)}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.telegram_description)}
                  </p>
                  {telegramIntegration && (
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.integrations.telegram_connected_as)}{" "}
                      <span className="font-medium">@{telegramIntegration.bot_username}</span>
                    </p>
                  )}
                </div>
              </div>
              {canManage && telegramIntegration && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleTelegramDisconnect}
                  disabled={deleteTelegram.isPending}
                >
                  {deleteTelegram.isPending
                    ? t(($) => $.integrations.disconnecting)
                    : t(($) => $.integrations.disconnect)}
                </Button>
              )}
            </div>

            {/* Admin: bot token form (when not yet connected) */}
            {canManage && !telegramIntegration && (
              <div className="space-y-2">
                <p className="text-xs font-medium">{t(($) => $.integrations.telegram_bot_token_label)}</p>
                <div className="flex items-center gap-2">
                  <Input
                    className="h-8 text-xs font-mono"
                    placeholder={t(($) => $.integrations.telegram_bot_token_placeholder)}
                    value={telegramTokenInput}
                    onChange={(e) => setTelegramTokenInput(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleTelegramSave()}
                  />
                  <Button
                    size="sm"
                    onClick={handleTelegramSave}
                    disabled={!telegramTokenInput.trim() || upsertTelegram.isPending}
                  >
                    {upsertTelegram.isPending
                      ? t(($) => $.integrations.saving)
                      : t(($) => $.integrations.save)}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">{t(($) => $.integrations.telegram_bot_token_hint)}</p>
              </div>
            )}

            {/* Member account linking */}
            <div className="border-t pt-4 space-y-2">
              <p className="text-xs font-medium">{t(($) => $.integrations.personal_link_title)}</p>
              {telegramLinked ? (
                <div className="flex items-center gap-2">
                  <p className="text-xs text-muted-foreground">{t(($) => $.integrations.telegram_you_are_linked)}</p>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={handleTelegramUnlink}
                    disabled={unlinkTelegram.isPending}
                  >
                    {t(($) => $.integrations.unlink)}
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <Input
                    className="h-8 text-xs"
                    placeholder={t(($) => $.integrations.telegram_chat_id_placeholder)}
                    value={telegramChatIdInput}
                    onChange={(e) => setTelegramChatIdInput(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleTelegramLink()}
                  />
                  <Button
                    size="sm"
                    onClick={handleTelegramLink}
                    disabled={!telegramChatIdInput.trim() || linkTelegram.isPending}
                  >
                    {linkTelegram.isPending
                      ? t(($) => $.integrations.linking)
                      : t(($) => $.integrations.link_account)}
                  </Button>
                </div>
              )}
              <p className="text-xs text-muted-foreground">{t(($) => $.integrations.telegram_chat_id_hint)}</p>
            </div>

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>

        {/* AI Provider */}
        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start gap-3">
              <BotMark className="h-6 w-6 mt-0.5 shrink-0" />
              <div className="space-y-1">
                <p className="text-sm font-medium">{t(($) => $.integrations.ai_provider_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.integrations.ai_provider_description)}
                </p>
                {aiProviderData ? (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.ai_provider_current)}{" "}
                    <span className="font-medium">{aiProviderData.provider}</span>
                  </p>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.ai_provider_not_configured)}
                  </p>
                )}
              </div>
            </div>

            {canManage ? (
              <div className="space-y-3">
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.integrations.ai_provider_label)}</p>
                  <select
                    className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs"
                    value={aiProvider}
                    onChange={(e) => setAiProvider(e.target.value)}
                  >
                    <option value="anthropic">{t(($) => $.integrations.ai_provider_provider_anthropic)}</option>
                    <option value="openai">{t(($) => $.integrations.ai_provider_provider_openai)}</option>
                    <option value="gemini">{t(($) => $.integrations.ai_provider_provider_gemini)}</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.integrations.ai_provider_model_label)}</p>
                  <Input
                    className="h-8 text-xs"
                    value={aiModel}
                    onChange={(e) => setAiModel(e.target.value)}
                    placeholder={aiProviderData?.model ?? ""}
                  />
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium">{t(($) => $.integrations.ai_provider_key_label)}</p>
                  <Input
                    className="h-8 text-xs font-mono"
                    type="password"
                    value={aiKey}
                    onChange={(e) => setAiKey(e.target.value)}
                    placeholder={
                      aiProviderData?.has_api_key
                        ? t(($) => $.integrations.ai_provider_key_set)
                        : t(($) => $.integrations.ai_provider_key_placeholder)
                    }
                  />
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    onClick={handleAIProviderSave}
                    disabled={upsertAIProvider.isPending}
                  >
                    {upsertAIProvider.isPending
                      ? t(($) => $.integrations.ai_provider_saving)
                      : t(($) => $.integrations.ai_provider_save)}
                  </Button>
                  {aiProviderData && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleAIProviderRemove}
                      disabled={deleteAIProvider.isPending}
                    >
                      {t(($) => $.integrations.ai_provider_remove)}
                    </Button>
                  )}
                </div>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
