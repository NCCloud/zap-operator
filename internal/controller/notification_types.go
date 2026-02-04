package controller

import (
	"context"
	"time"
)

const (
	HIGH_LEVEL_EMOJI   = ":red_circle:"
	MEDIUM_LEVEL_EMOJI = ":large_orange_circle:"
	LOW_LEVEL_EMOJI    = ":large_yellow_circle:"
	DEFAULT_EMOJI      = ":white_circle:"
)

type Notifier interface {
	Send(ctx context.Context, notification ScanNotification) error
}

type SlackNotifier struct {
	WebhookURL string
}
type ScanNotification struct {
	ScanName    string
	Namespace   string
	Target      string
	Phase       string
	TotalAlerts int
	Alerts      []AlertSummary
	Duration    time.Duration
}

type AlertSummary struct {
	PluginID string
	Risk     string
	Count    int
}

type EmailNotifier struct {
	Endpoint string
}

type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type   string      `json:"type"`
	Text   *slackText  `json:"text,omitempty"`
	Fields []slackText `json:"fields,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
