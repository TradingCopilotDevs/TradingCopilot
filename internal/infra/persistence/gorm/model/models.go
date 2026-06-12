package model

func Models() []any {
	return []any{
		&AdminUser{}, &AuthSession{}, &AuditEvent{}, &Secret{}, &AppSetting{}, &AiProvider{}, &AiProviderModel{}, &AgentRole{}, &PromptTemplate{},
		&ResearchTeam{}, &ResearchTeamRole{},
		&MarketSymbol{}, &DailyBar{}, &RealtimeQuote{}, &WatchlistItem{},
		&PredictionEvent{}, &PredictionMarket{}, &PredictionMarketQuote{}, &PredictionMarketMatch{}, &PredictionWatchlistItem{},
		&MessageSubscriptionFilter{}, &MessageSubscription{}, &MessageSubscriptionResearchTeam{}, &IngestedMessage{}, &PlatformAdapter{},
		&TelegramChannel{}, &TelegramMessage{},
		&Meeting{}, &MeetingEvent{}, &MeetingReference{}, &ToolCallLog{}, &WakePlan{},
		&RiskConfig{}, &PaperAccount{}, &PaperOrder{}, &PaperPosition{}, &PaperFill{}, &PaperEquitySnapshot{}, &PaperCorporateAction{},
	}
}
