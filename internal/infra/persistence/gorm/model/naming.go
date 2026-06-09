package model

import "gorm.io/gorm/schema"

type NamingStrategy struct {
	Delegate schema.Namer
}

var tableNameOverrides = map[string]string{
	"Provider":        "ai_providers",
	"ProviderModel":   "ai_provider_models",
	"Symbol":          "market_symbols",
	"Channel":         "telegram_channels",
	"Message":         "telegram_messages",
	"Event":           "meeting_events",
	"Reference":       "meeting_references",
	"Plan":            "wake_plans",
	"Account":         "paper_accounts",
	"Order":           "paper_orders",
	"Position":        "paper_positions",
	"Fill":            "paper_fills",
	"EquitySnapshot":  "paper_equity_snapshots",
	"CorporateAction": "paper_corporate_actions",
}

func NewNamingStrategy(delegate schema.Namer) NamingStrategy {
	if delegate == nil {
		delegate = schema.NamingStrategy{}
	}
	return NamingStrategy{Delegate: delegate}
}

func (n NamingStrategy) TableName(str string) string {
	if table, ok := tableNameOverrides[str]; ok {
		return table
	}
	return n.Delegate.TableName(str)
}

func (n NamingStrategy) SchemaName(table string) string {
	return n.Delegate.SchemaName(table)
}

func (n NamingStrategy) ColumnName(table string, column string) string {
	return n.Delegate.ColumnName(table, column)
}

func (n NamingStrategy) JoinTableName(str string) string {
	return n.Delegate.JoinTableName(str)
}

func (n NamingStrategy) RelationshipFKName(rel schema.Relationship) string {
	return n.Delegate.RelationshipFKName(rel)
}

func (n NamingStrategy) CheckerName(table string, column string) string {
	return n.Delegate.CheckerName(table, column)
}

func (n NamingStrategy) IndexName(table string, column string) string {
	return n.Delegate.IndexName(table, column)
}

func (n NamingStrategy) UniqueName(table string, column string) string {
	return n.Delegate.UniqueName(table, column)
}
