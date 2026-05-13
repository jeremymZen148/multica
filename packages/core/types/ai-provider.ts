export type AIProvider = "anthropic" | "openai" | "gemini";

export interface AIProviderConfig {
  provider: AIProvider;
  model: string;
  has_api_key: boolean;
}

export interface AIProviderConfigInput {
  provider: AIProvider;
  model: string;
  api_key: string;
}
