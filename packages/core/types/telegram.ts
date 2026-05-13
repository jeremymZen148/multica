export interface TelegramIntegration {
  id: string;
  workspace_id: string;
  bot_username: string;
  created_at: string;
}

export interface TelegramUserLink {
  user_id: string;
  telegram_chat_id: number;
  telegram_username: string | null;
}
