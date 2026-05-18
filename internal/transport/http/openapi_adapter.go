package httptransport

import (
	"net/http"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/openapi"
)

var _ openapi.ServerInterface = (*Server)(nil)

func (s *Server) GetAiProviders(w http.ResponseWriter, r *http.Request, _ openapi.GetAiProvidersParams) {
	s.listProviders(w, r)
}
func (s *Server) PostAiProviders(w http.ResponseWriter, r *http.Request) { s.createProvider(w, r) }
func (s *Server) PutAiProvider(w http.ResponseWriter, r *http.Request, _ openapi.ProviderId) {
	s.updateProvider(w, r)
}
func (s *Server) GetAiProviderModels(w http.ResponseWriter, r *http.Request, _ openapi.ProviderId, _ openapi.GetAiProviderModelsParams) {
	s.listProviderModels(w, r)
}
func (s *Server) PostAiProviderModelsSync(w http.ResponseWriter, r *http.Request, _ openapi.ProviderId) {
	s.syncProviderModels(w, r)
}
func (s *Server) GetAiRoles(w http.ResponseWriter, r *http.Request, _ openapi.GetAiRolesParams) {
	s.listRoles(w, r)
}
func (s *Server) PostAiRolesApplyDefaultCapabilities(w http.ResponseWriter, r *http.Request) {
	s.applyDefaultRoleCapabilities(w, r)
}
func (s *Server) PutAiRole(w http.ResponseWriter, r *http.Request, _ openapi.RoleKey) {
	s.upsertRole(w, r)
}
func (s *Server) GetResearchTeams(w http.ResponseWriter, r *http.Request) {
	s.listResearchTeams(w, r)
}
func (s *Server) PostResearchTeams(w http.ResponseWriter, r *http.Request) {
	s.createResearchTeam(w, r)
}
func (s *Server) PutResearchTeam(w http.ResponseWriter, r *http.Request, _ openapi.TeamId) {
	s.updateResearchTeam(w, r)
}
func (s *Server) DeleteResearchTeam(w http.ResponseWriter, r *http.Request, _ openapi.TeamId) {
	s.deleteResearchTeam(w, r)
}
func (s *Server) GetResearchTeamRoles(w http.ResponseWriter, r *http.Request, _ openapi.TeamId) {
	s.listResearchTeamRoles(w, r)
}
func (s *Server) PostResearchTeamRolesApplyDefaults(w http.ResponseWriter, r *http.Request, _ openapi.TeamId) {
	s.applyDefaultResearchTeamRoles(w, r)
}
func (s *Server) PutResearchTeamRole(w http.ResponseWriter, r *http.Request, _ openapi.TeamId, _ openapi.RoleKey) {
	s.putResearchTeamRole(w, r)
}
func (s *Server) DeleteResearchTeamRole(w http.ResponseWriter, r *http.Request, _ openapi.TeamId, _ openapi.RoleKey) {
	s.deleteResearchTeamRole(w, r)
}
func (s *Server) GetAiSkillDefinitions(w http.ResponseWriter, r *http.Request, _ openapi.GetAiSkillDefinitionsParams) {
	s.skillDefinitions(w, r)
}
func (s *Server) GetAiToolDefinitions(w http.ResponseWriter, r *http.Request, _ openapi.GetAiToolDefinitionsParams) {
	s.toolDefinitions(w, r)
}
func (s *Server) PostAuthBootstrap(w http.ResponseWriter, r *http.Request) { s.bootstrap(w, r) }
func (s *Server) GetAuthBootstrapRequired(w http.ResponseWriter, r *http.Request) {
	s.bootstrapRequired(w, r)
}
func (s *Server) PostAuthLogin(w http.ResponseWriter, r *http.Request) { s.login(w, r) }
func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request)  { s.dashboard(w, r) }
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request, _ openapi.GetLogsParams) {
	s.listLogs(w, r)
}
func (s *Server) GetLogFiles(w http.ResponseWriter, r *http.Request) { s.listLogFiles(w, r) }
func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request)   { s.health(w, r) }
func (s *Server) GetMarketDaily(w http.ResponseWriter, r *http.Request, _ openapi.Code, _ openapi.GetMarketDailyParams) {
	s.dailyBars(w, r)
}
func (s *Server) GetMarketQuote(w http.ResponseWriter, r *http.Request, _ openapi.Code, _ openapi.GetMarketQuoteParams) {
	s.currentQuote(w, r)
}
func (s *Server) GetMarketSeries(w http.ResponseWriter, r *http.Request, _ openapi.Code, _ openapi.GetMarketSeriesParams) {
	s.marketSeries(w, r)
}
func (s *Server) GetMarketSymbols(w http.ResponseWriter, r *http.Request, _ openapi.GetMarketSymbolsParams) {
	s.listSymbols(w, r)
}
func (s *Server) PostMarketSymbols(w http.ResponseWriter, r *http.Request) { s.createSymbol(w, r) }
func (s *Server) PostMarketSymbolsSync(w http.ResponseWriter, r *http.Request) {
	s.syncMarketSymbols(w, r)
}
func (s *Server) DeleteMarketSymbol(w http.ResponseWriter, r *http.Request, _ openapi.Code) {
	s.deleteSymbol(w, r)
}
func (s *Server) PutMarketSymbol(w http.ResponseWriter, r *http.Request, _ openapi.Code) {
	s.updateSymbol(w, r)
}
func (s *Server) PostMarketToolsQuery(w http.ResponseWriter, r *http.Request) { s.toolQuery(w, r) }
func (s *Server) GetMessageSubscriptionAppConfig(w http.ResponseWriter, r *http.Request) {
	s.messageSubscriptionAppConfig(w, r)
}
func (s *Server) PostMessageSubscriptionAppConfig(w http.ResponseWriter, r *http.Request) {
	s.saveMessageSubscriptionAppConfig(w, r)
}
func (s *Server) GetMessageSubscriptions(w http.ResponseWriter, r *http.Request, _ openapi.GetMessageSubscriptionsParams) {
	s.listMessageSubscriptions(w, r)
}
func (s *Server) GetMessageSubscriptionFilters(w http.ResponseWriter, r *http.Request, _ openapi.GetMessageSubscriptionFiltersParams) {
	s.listMessageSubscriptionFilters(w, r)
}
func (s *Server) PostMessageSubscriptionFilters(w http.ResponseWriter, r *http.Request) {
	s.createMessageSubscriptionFilter(w, r)
}
func (s *Server) PutMessageSubscriptionFilter(w http.ResponseWriter, r *http.Request, _ openapi.FilterId) {
	s.updateMessageSubscriptionFilter(w, r)
}
func (s *Server) PostMessageSubscriptions(w http.ResponseWriter, r *http.Request) {
	s.createMessageSubscription(w, r)
}
func (s *Server) PostMessageSubscriptionsTest(w http.ResponseWriter, r *http.Request) {
	s.testMessageSubscriptionRef(w, r)
}
func (s *Server) PostMessageSubscriptionsCollect(w http.ResponseWriter, r *http.Request) {
	s.collectMessageSubscriptions(w, r)
}
func (s *Server) DeleteMessageSubscription(w http.ResponseWriter, r *http.Request, _ openapi.SubscriptionId) {
	s.deleteMessageSubscription(w, r)
}
func (s *Server) PutMessageSubscription(w http.ResponseWriter, r *http.Request, _ openapi.SubscriptionId) {
	s.updateMessageSubscription(w, r)
}
func (s *Server) PostMessageSubscriptionTest(w http.ResponseWriter, r *http.Request, _ openapi.SubscriptionId) {
	s.testMessageSubscription(w, r)
}
func (s *Server) PostMessageSubscriptionTelegramLoginStart(w http.ResponseWriter, r *http.Request) {
	s.messageSubscriptionLoginStart(w, r)
}
func (s *Server) PostMessageSubscriptionTelegramLoginVerify(w http.ResponseWriter, r *http.Request) {
	s.messageSubscriptionLoginVerify(w, r)
}
func (s *Server) GetPlatformAdapters(w http.ResponseWriter, r *http.Request) {
	s.listPlatformAdapters(w, r)
}
func (s *Server) PostPlatformAdapters(w http.ResponseWriter, r *http.Request) {
	s.createPlatformAdapter(w, r)
}
func (s *Server) DeletePlatformAdapter(w http.ResponseWriter, r *http.Request, _ openapi.AdapterId) {
	s.deletePlatformAdapter(w, r)
}
func (s *Server) PutPlatformAdapter(w http.ResponseWriter, r *http.Request, _ openapi.AdapterId) {
	s.updatePlatformAdapter(w, r)
}
func (s *Server) PostPlatformAdapterTest(w http.ResponseWriter, r *http.Request, _ openapi.AdapterId) {
	s.testPlatformAdapter(w, r)
}
func (s *Server) GetIngestedMessages(w http.ResponseWriter, r *http.Request, _ openapi.GetIngestedMessagesParams) {
	s.listIngestedMessages(w, r)
}
func (s *Server) PostIngestedMessages(w http.ResponseWriter, r *http.Request) {
	s.createIngestedMessage(w, r)
}
func (s *Server) PostIngestedMessagesRefilter(w http.ResponseWriter, r *http.Request) {
	s.refilterIngestedMessages(w, r)
}
func (s *Server) DeleteIngestedMessage(w http.ResponseWriter, r *http.Request, _ openapi.MessageId) {
	s.deleteIngestedMessage(w, r)
}
func (s *Server) PutIngestedMessage(w http.ResponseWriter, r *http.Request, _ openapi.MessageId) {
	s.updateIngestedMessage(w, r)
}
func (s *Server) PostIngestedMessageFilter(w http.ResponseWriter, r *http.Request, _ openapi.MessageId) {
	s.filterIngestedMessage(w, r)
}
func (s *Server) GetMarketWatchlist(w http.ResponseWriter, r *http.Request, _ openapi.GetMarketWatchlistParams) {
	s.listWatchlist(w, r)
}
func (s *Server) PostMarketWatchlist(w http.ResponseWriter, r *http.Request) {
	s.upsertWatchlist(w, r)
}
func (s *Server) DeleteMarketWatchlistItem(w http.ResponseWriter, r *http.Request, _ openapi.ItemId) {
	s.deleteWatchlist(w, r)
}
func (s *Server) PutMarketWatchlistItem(w http.ResponseWriter, r *http.Request, _ openapi.ItemId) {
	s.updateWatchlist(w, r)
}
func (s *Server) GetMeetings(w http.ResponseWriter, r *http.Request, _ openapi.GetMeetingsParams) {
	s.listMeetings(w, r)
}
func (s *Server) PostMeetings(w http.ResponseWriter, r *http.Request) { s.startMeeting(w, r) }
func (s *Server) DeleteMeeting(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.deleteMeeting(w, r)
}
func (s *Server) GetMeeting(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.getMeeting(w, r)
}
func (s *Server) PutMeeting(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.updateMeeting(w, r)
}
func (s *Server) PostMeetingCancel(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.cancelMeeting(w, r)
}
func (s *Server) GetMeetingEvents(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId, _ openapi.GetMeetingEventsParams) {
	s.listEvents(w, r)
}
func (s *Server) PostMeetingRecap(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.recapMeeting(w, r)
}
func (s *Server) GetMeetingReferences(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId, _ openapi.GetMeetingReferencesParams) {
	s.listReferences(w, r)
}
func (s *Server) PostMeetingReferences(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.createReference(w, r)
}
func (s *Server) DeleteMeetingReference(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId, _ openapi.ReferenceId) {
	s.deleteReference(w, r)
}
func (s *Server) PostMeetingRestart(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.restartMeeting(w, r)
}
func (s *Server) GetMeetingStream(w http.ResponseWriter, r *http.Request, _ openapi.MeetingId) {
	s.streamEvents(w, r)
}
func (s *Server) GetPaperAccounts(w http.ResponseWriter, r *http.Request, _ openapi.GetPaperAccountsParams) {
	s.listPaperAccounts(w, r)
}
func (s *Server) PostPaperAccounts(w http.ResponseWriter, r *http.Request) {
	s.createPaperAccount(w, r)
}
func (s *Server) DeletePaperAccount(w http.ResponseWriter, r *http.Request, _ openapi.AccountId) {
	s.deletePaperAccount(w, r)
}
func (s *Server) PutPaperAccount(w http.ResponseWriter, r *http.Request, _ openapi.AccountId) {
	s.updatePaperAccount(w, r)
}
func (s *Server) PostPaperAccountActivate(w http.ResponseWriter, r *http.Request, _ openapi.AccountId) {
	s.activatePaperAccount(w, r)
}
func (s *Server) PostPaperAccountDeactivate(w http.ResponseWriter, r *http.Request, _ openapi.AccountId) {
	s.deactivatePaperAccount(w, r)
}
func (s *Server) GetPaperAccountFills(w http.ResponseWriter, r *http.Request, _ openapi.AccountId, _ openapi.GetPaperAccountFillsParams) {
	s.listFills(w, r)
}
func (s *Server) GetPaperAccountOrders(w http.ResponseWriter, r *http.Request, _ openapi.AccountId, _ openapi.GetPaperAccountOrdersParams) {
	s.listAccountOrders(w, r)
}
func (s *Server) GetPaperAccountPerformance(w http.ResponseWriter, r *http.Request, _ openapi.AccountId) {
	s.paperPerformance(w, r)
}
func (s *Server) GetPaperAccountPositions(w http.ResponseWriter, r *http.Request, _ openapi.AccountId, _ openapi.GetPaperAccountPositionsParams) {
	s.listPositions(w, r)
}
func (s *Server) GetPaperOrders(w http.ResponseWriter, r *http.Request, _ openapi.GetPaperOrdersParams) {
	s.listOrders(w, r)
}
func (s *Server) PostPaperOrders(w http.ResponseWriter, r *http.Request) { s.createOrder(w, r) }
func (s *Server) DeletePaperOrder(w http.ResponseWriter, r *http.Request, _ openapi.OrderId) {
	s.deleteOrder(w, r)
}
func (s *Server) PostPaperOrderCancel(w http.ResponseWriter, r *http.Request, _ openapi.OrderId) {
	s.cancelOrder(w, r)
}
func (s *Server) PostPaperOrderFill(w http.ResponseWriter, r *http.Request, _ openapi.OrderId) {
	s.fillOrder(w, r)
}
func (s *Server) GetPaperOverview(w http.ResponseWriter, r *http.Request) { s.paperOverview(w, r) }
func (s *Server) GetPaperRiskConfigs(w http.ResponseWriter, r *http.Request, _ openapi.GetPaperRiskConfigsParams) {
	s.listRiskConfigs(w, r)
}
func (s *Server) PostPaperRiskConfigs(w http.ResponseWriter, r *http.Request) {
	s.createRiskConfig(w, r)
}
func (s *Server) DeletePaperRiskConfig(w http.ResponseWriter, r *http.Request, _ openapi.ConfigId) {
	s.deleteRiskConfig(w, r)
}
func (s *Server) PutPaperRiskConfig(w http.ResponseWriter, r *http.Request, _ openapi.ConfigId) {
	s.updateRiskConfig(w, r)
}
func (s *Server) GetSettingsAppSettings(w http.ResponseWriter, r *http.Request, _ openapi.GetSettingsAppSettingsParams) {
	s.listAppSettings(w, r)
}
func (s *Server) PutSettingsAppSetting(w http.ResponseWriter, r *http.Request, _ openapi.Key) {
	s.upsertAppSetting(w, r)
}
func (s *Server) GetSettingsProxy(w http.ResponseWriter, r *http.Request) { s.proxySettings(w, r) }
func (s *Server) PutSettingsProxy(w http.ResponseWriter, r *http.Request) { s.saveProxySettings(w, r) }
func (s *Server) PostSettingsProxyTest(w http.ResponseWriter, r *http.Request) {
	s.testProxySettings(w, r)
}
func (s *Server) GetSettingsRuntimeEnv(w http.ResponseWriter, r *http.Request, _ openapi.GetSettingsRuntimeEnvParams) {
	s.runtimeEnv(w, r)
}
func (s *Server) GetSettingsSecretDefinitions(w http.ResponseWriter, r *http.Request, _ openapi.GetSettingsSecretDefinitionsParams) {
	s.secretDefinitions(w, r)
}
func (s *Server) GetSettingsSecrets(w http.ResponseWriter, r *http.Request, _ openapi.GetSettingsSecretsParams) {
	s.listSecrets(w, r)
}
func (s *Server) PostSettingsSecrets(w http.ResponseWriter, r *http.Request) { s.saveSecret(w, r) }
func (s *Server) GetWakePlans(w http.ResponseWriter, r *http.Request, _ openapi.GetWakePlansParams) {
	s.listWakePlans(w, r)
}
func (s *Server) PostWakePlans(w http.ResponseWriter, r *http.Request) { s.createWakePlan(w, r) }
func (s *Server) DeleteWakePlan(w http.ResponseWriter, r *http.Request, _ openapi.PlanId) {
	s.deleteWakePlan(w, r)
}
func (s *Server) PostWakePlanCancel(w http.ResponseWriter, r *http.Request, _ openapi.PlanId) {
	s.cancelWakePlan(w, r)
}
func (s *Server) PostWakePlanFire(w http.ResponseWriter, r *http.Request, _ openapi.PlanId) {
	s.fireWakePlan(w, r)
}
func (s *Server) PostWakePlanPause(w http.ResponseWriter, r *http.Request, _ openapi.PlanId) {
	s.pauseWakePlan(w, r)
}
func (s *Server) PostWakePlanResume(w http.ResponseWriter, r *http.Request, _ openapi.PlanId) {
	s.resumeWakePlan(w, r)
}
