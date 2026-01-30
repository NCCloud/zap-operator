package controller

import (
	"context"
	"errors"
	"testing"
)

// podExecRunnerFunc is a test helper that implements podExecRunner interface
type podExecRunnerFunc func(ctx context.Context, namespace, podName, container string, command []string) (stdout []byte, stderr []byte, err error)

func (f podExecRunnerFunc) execInPod(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
	return f(ctx, namespace, podName, container, command)
}

func TestReadFileFromPod_Success(t *testing.T) {
	ctx := context.Background()

	expectedContent := []byte("file content here")

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			// Verify the command is correctly formed
			if len(command) != 3 {
				t.Errorf("expected 3 command parts, got %d", len(command))
			}
			if command[0] != "/bin/sh" || command[1] != "-c" {
				t.Errorf("unexpected command prefix: %v", command[:2])
			}
			return expectedContent, nil, nil
		}),
	}

	content, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/path/to/file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != string(expectedContent) {
		t.Errorf("expected %q, got %q", expectedContent, content)
	}
}

func TestReadFileFromPod_ExecError(t *testing.T) {
	ctx := context.Background()

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			return nil, []byte("connection refused"), errors.New("exec error")
		}),
	}

	_, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/path/to/file")
	if err == nil {
		t.Error("expected error when exec fails")
	}
	// Check that the error message contains both the error and stderr
	if !containsSubstring(err.Error(), "exec failed") {
		t.Errorf("error should contain 'exec failed': %v", err)
	}
	if !containsSubstring(err.Error(), "connection refused") {
		t.Errorf("error should contain stderr: %v", err)
	}
}

func TestReadFileFromPod_FileNotFound(t *testing.T) {
	ctx := context.Background()

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			// Return empty stdout (file doesn't exist)
			return []byte{}, []byte(""), nil
		}),
	}

	_, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/nonexistent/file")
	if err == nil {
		t.Error("expected error when file not found")
	}
	if !containsSubstring(err.Error(), "file not found or empty") {
		t.Errorf("error should indicate file not found: %v", err)
	}
}

func TestReadFileFromPod_EmptyFile(t *testing.T) {
	ctx := context.Background()

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			// Return empty stdout (file is empty)
			return []byte{}, nil, nil
		}),
	}

	_, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/empty/file")
	if err == nil {
		t.Error("expected error when file is empty")
	}
}

func TestReadFileFromPod_CommandConstruction(t *testing.T) {
	ctx := context.Background()
	filePath := "/path/with spaces/file.json"

	var capturedCommand []string
	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			capturedCommand = command
			return []byte("content"), nil, nil
		}),
	}

	_, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "zap", filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify command structure
	if len(capturedCommand) != 3 {
		t.Fatalf("expected 3 command parts, got %d", len(capturedCommand))
	}
	if capturedCommand[0] != "/bin/sh" {
		t.Errorf("expected /bin/sh, got %s", capturedCommand[0])
	}
	if capturedCommand[1] != "-c" {
		t.Errorf("expected -c, got %s", capturedCommand[1])
	}
	// The command should contain test and cat with the quoted filepath
	if !containsSubstring(capturedCommand[2], "test -f") || !containsSubstring(capturedCommand[2], "cat") {
		t.Errorf("unexpected command: %s", capturedCommand[2])
	}
}

func TestReadFileFromPod_WithStderr(t *testing.T) {
	ctx := context.Background()

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			return []byte("content"), []byte("some warning"), nil
		}),
	}

	// Should succeed even with stderr output if stdout has content
	content, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/path/to/file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "content" {
		t.Errorf("expected 'content', got %q", content)
	}
}

func TestReadFileFromPod_ExecErrorWithPartialOutput(t *testing.T) {
	ctx := context.Background()

	r := &ScanReconciler{
		execRunner: podExecRunnerFunc(func(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
			// Simulate partial output before error
			return []byte("partial"), []byte("error occurred"), errors.New("command failed")
		}),
	}

	_, err := r.readFileFromPod(ctx, "test-ns", "test-pod", "test-container", "/path/to/file")
	if err == nil {
		t.Error("expected error")
	}
	// Error should include stderr content
	if !containsSubstring(err.Error(), "error occurred") {
		t.Errorf("error should contain stderr: %v", err)
	}
}
