package controller

import "context"

type podLogsGetter interface {
	getPodLogs(ctx context.Context, namespace, podName, container string) ([]byte, error)
}

type podExecGetter interface {
	readFileFromPod(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error)
}

type podExecRunner interface {
	execInPod(ctx context.Context, namespace, podName, container string, command []string) (stdout []byte, stderr []byte, err error)
}
