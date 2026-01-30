package controller

import "context"

type podLogsGetterFunc func(ctx context.Context, namespace, podName, container string) ([]byte, error)

func (f podLogsGetterFunc) getPodLogs(ctx context.Context, namespace, podName, container string) ([]byte, error) {
	return f(ctx, namespace, podName, container)
}
