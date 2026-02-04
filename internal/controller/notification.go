package controller

type SlackMessage struct {
	Text string `json:"text"`
}

func (r *ScanReconciler) sendNotification(webhookUrl string, alerts *parsedAlerts) error {

	// http://ghcr.io/nccloud/zap-operator:0.1.1
	return nil
}
