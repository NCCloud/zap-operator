package controller

import "context"

type podLogsGetter interface {
	getPodLogs(ctx context.Context, namespace, podName, container string) ([]byte, error)
}
