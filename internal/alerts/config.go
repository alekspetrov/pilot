package alerts

import (
	"github.com/alekspetrov/pilot/internal/config"
)

// FromConfigAlerts converts config.AlertsConfig to alerts.AlertConfig
func FromConfigAlerts(cfg *config.AlertsConfig) *AlertConfig {
	alertCfg := &AlertConfig{
		Enabled:  cfg.Enabled,
		Channels: make([]ChannelConfig, 0, len(cfg.Channels)),
		Rules:    make([]AlertRule, 0, len(cfg.Rules)),
		Defaults: AlertDefaults{
			Cooldown:           cfg.Defaults.Cooldown,
			DefaultSeverity:    parseSeverity(cfg.Defaults.DefaultSeverity),
			SuppressDuplicates: cfg.Defaults.SuppressDuplicates,
		},
	}

	for _, ch := range cfg.Channels {
		alertCfg.Channels = append(alertCfg.Channels, convertChannel(ch))
	}

	for _, r := range cfg.Rules {
		alertCfg.Rules = append(alertCfg.Rules, convertRule(r))
	}

	return alertCfg
}

func convertChannel(in config.AlertChannelConfig) ChannelConfig {
	ch := ChannelConfig{
		Name:       in.Name,
		Type:       in.Type,
		Enabled:    in.Enabled,
		Severities: make([]Severity, 0, len(in.Severities)),
		Slack:      in.Slack,
		Telegram:   in.Telegram,
		Email:      in.Email,
		Webhook:    in.Webhook,
		PagerDuty:  in.PagerDuty,
	}

	for _, s := range in.Severities {
		ch.Severities = append(ch.Severities, parseSeverity(s))
	}

	return ch
}

func convertRule(in config.AlertRuleConfig) AlertRule {
	return AlertRule{
		Name:        in.Name,
		Type:        parseAlertType(in.Type),
		Enabled:     in.Enabled,
		Severity:    parseSeverity(in.Severity),
		Channels:    in.Channels,
		Cooldown:    in.Cooldown,
		Description: in.Description,
		Condition: RuleCondition{
			ProgressUnchangedFor: in.Condition.ProgressUnchangedFor,
			ConsecutiveFailures:  in.Condition.ConsecutiveFailures,
			DailySpendThreshold:  in.Condition.DailySpendThreshold,
			BudgetLimit:          in.Condition.BudgetLimit,
			UsageSpikePercent:    in.Condition.UsageSpikePercent,
			Pattern:              in.Condition.Pattern,
			FilePattern:          in.Condition.FilePattern,
			Paths:                in.Condition.Paths,
		},
	}
}

func parseSeverity(s string) Severity {
	switch s {
	case "critical":
		return SeverityCritical
	case "warning":
		return SeverityWarning
	case "info":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

func parseAlertType(t string) AlertType {
	switch t {
	case "task_stuck":
		return AlertTypeTaskStuck
	case "task_failed":
		return AlertTypeTaskFailed
	case "consecutive_failures":
		return AlertTypeConsecutiveFails
	case "service_unhealthy":
		return AlertTypeServiceUnhealthy
	case "daily_spend_exceeded":
		return AlertTypeDailySpend
	case "budget_depleted":
		return AlertTypeBudgetDepleted
	case "usage_spike":
		return AlertTypeUsageSpike
	case "unauthorized_access":
		return AlertTypeUnauthorizedAccess
	case "sensitive_file_modified":
		return AlertTypeSensitiveFile
	case "unusual_pattern":
		return AlertTypeUnusualPattern
	default:
		return AlertType(t)
	}
}
