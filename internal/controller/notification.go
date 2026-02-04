package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

// generic notification client
func NewNotifier(ctx context.Context, c client.Client, namespace string, spec zapv1alpha1.NotificationSpec) (Notifier, error) {
	endpoint, err := resolveEndpoint(ctx, c, namespace, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve notification endpoint: %w", err)
	}

	switch spec.Protocol {
	case "slack", "":
		return &SlackNotifier{WebhookURL: endpoint}, nil
	case "email":
		return &EmailNotifier{Endpoint: endpoint}, nil
	case "pdf":
		return &PDFNotifier{Endpoint: endpoint}, nil
	default:
		return nil, fmt.Errorf("unsupported notification protocol: %s", spec.Protocol)
	}
}

func resolveEndpoint(ctx context.Context, c client.Client, namespace string, spec zapv1alpha1.NotificationSpec) (string, error) {
	if spec.SecretRef != nil {
		var secret corev1.Secret
		secretKey := types.NamespacedName{
			Name:      spec.SecretRef.Name,
			Namespace: namespace,
		}
		if err := c.Get(ctx, secretKey, &secret); err != nil {
			return "", fmt.Errorf("failed to get secret %s: %w", spec.SecretRef.Name, err)
		}

		value, ok := secret.Data[spec.SecretRef.Key]
		if !ok {
			return "", fmt.Errorf("key %s not found in secret %s", spec.SecretRef.Key, spec.SecretRef.Name)
		}
		return string(value), nil
	}

	if spec.Url == "" {
		return "", fmt.Errorf("no URL or SecretRef provided for notification")
	}
	return spec.Url, nil
}

func (s *SlackNotifier) Send(ctx context.Context, n ScanNotification) error {
	msg := buildSlackMessage(n)

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (e *EmailNotifier) Send(ctx context.Context, n ScanNotification) error {
	// TODO: Implement email notification
	return fmt.Errorf("email notification not yet implemented")
}

// PDFNotifier generates PDF reports (placeholder for future implementation).
type PDFNotifier struct {
	Endpoint string
}

func (p *PDFNotifier) Send(ctx context.Context, n ScanNotification) error {
	// TODO: Implement PDF generation/notification
	return fmt.Errorf("pdf notification not yet implemented")
}

func buildSlackMessage(n ScanNotification) slackMessage {
	statusEmoji := ":white_check_mark:"
	if n.Phase == "Failed" {
		statusEmoji = ":x:"
	}

	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{
				Type: "plain_text",
				Text: fmt.Sprintf("ZAP Scan: %s", n.ScanName),
			},
		},
		{
			Type: "section",
			Fields: []slackText{
				{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s %s", statusEmoji, n.Phase)},
				{Type: "mrkdwn", Text: fmt.Sprintf("*Target:*\n%s", n.Target)},
				{Type: "mrkdwn", Text: fmt.Sprintf("*Namespace:*\n%s", n.Namespace)},
				{Type: "mrkdwn", Text: fmt.Sprintf("*Duration:*\n%s", n.Duration.Round(time.Second))},
			},
		},
		{Type: "divider"},
		{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Total Alerts:* %d", n.TotalAlerts),
			},
		},
	}

	if len(n.Alerts) > 0 {
		alertText := "*Alert Breakdown:*\n"
		for _, a := range n.Alerts {
			alertText += fmt.Sprintf("%s %s (%s): %d\n", riskEmoji(a.Risk), a.Risk, a.PluginID, a.Count)
		}

		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: alertText},
		})
	}

	return slackMessage{Blocks: blocks}
}

func riskEmoji(risk string) string {
	switch risk {
	case "high":
		return HIGH_LEVEL_EMOJI
	case "medium":
		return MEDIUM_LEVEL_EMOJI
	case "low":
		return LOW_LEVEL_EMOJI
	default:
		return DEFAULT_EMOJI
	}
}
