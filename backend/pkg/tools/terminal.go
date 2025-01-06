package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/docker"

	"github.com/docker/docker/api/types"
	"github.com/sirupsen/logrus"
)

const (
	defaultExecCommandTimeout = 5 * time.Minute
	defaultExtraExecTimeout   = 5 * time.Second

	ansiColorInputCmd    = "\033[96m"
	ansiColorSystemMsg   = "\033[92m"
	ansiColorReset       = "\033[0m"
	ansiLineTerminator   = "\r\n"
)

type terminal struct {
	flowID       int64
	containerID  int64
	containerLID string
	dockerClient docker.DockerClient
	tlp          TermLogProvider
}

func (t *terminal) wrapCommandResult(ctx context.Context, name, result string, err error) (string, error) {
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
			"tool":   name,
			"result": result[:min(len(result), 1000)],
		}).Error("terminal tool failed")
		return fmt.Sprintf("terminal tool '%s' handled with error: %v", name, err), nil
	}
	return result, nil
}

func (t *terminal) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	logger := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"tool": name,
		"args": string(args),
	})

	switch name {
	case TerminalToolName:
		var action TerminalAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal terminal action")
			return "", fmt.Errorf("failed to unmarshal terminal action: %w", err)
		}
		timeout := time.Duration(action.Timeout)*time.Second + defaultExtraExecTimeout
		result, err := t.ExecCommand(ctx, action.Cwd, action.Input, action.Detach.Bool(), timeout)
		return t.wrapCommandResult(ctx, name, result, err)
	case FileToolName:
		var action FileAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal file action")
			return "", fmt.Errorf("failed to unmarshal file action: %w", err)
		}

		logger = logger.WithFields(logrus.Fields{
			"action": action.Action,
			"path":   action.Path,
		})

		switch action.Action {
		case ReadFile:
			result, err := t.ReadFile(ctx, t.flowID, action.Path)
			return t.wrapCommandResult(ctx, name, result, err)
		case UpdateFile:
			result, err := t.WriteFile(ctx, t.flowID, action.Content, action.Path)
			return t.wrapCommandResult(ctx, name, result, err)
		default:
			logger.Error("unknown file action")
			return "", fmt.Errorf("unknown file action: %s", action.Action)
		}
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (t *terminal) ExecCommand(
	ctx context.Context,
	cwd, command string,
	detach bool,
	timeout time.Duration,
) (string, error) {
	container := PrimaryTerminalName(t.flowID)

	// create options for starting the exec process
	cmd := []string{
		"sh",
		"-c",
		command,
	}

	// check if container is running
	isRunning, err := t.dockerClient.VerifyContainerRuntime(ctx, t.containerLID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	if !isRunning {
		return "", fmt.Errorf("container is not running")
	}

	if cwd == "" {
		cwd = docker.WorkFolderPathInContainer
	}

	styledCommand := fmt.Sprintf("%s $ %s%s%s%s", cwd, ansiColorInputCmd, command, ansiColorReset, ansiLineTerminator)
	_, err = t.tlp.PutMsg(ctx, database.TermlogTypeStdin, styledCommand, t.containerID)
	if err != nil {
		return "", fmt.Errorf("failed to put terminal log (stdin): %w", err)
	}

	if timeout <= 0 || timeout > 20*time.Minute {
		timeout = defaultExecCommandTimeout
	}

	createResp, err := t.dockerClient.ContainerExecCreate(ctx, container, types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   cwd,
		Tty:          true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create exec process: %w", err)
	}

	if detach {
		go func() {
			_, _ = t.getExecResult(ctx, createResp.ID, timeout)
		}()
		return "Command executed successfully in the background mode", nil
	}

	return t.getExecResult(ctx, createResp.ID, timeout)
}

func (t *terminal) getExecResult(ctx context.Context, id string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// attach to the exec process
	resp, err := t.dockerClient.ContainerExecAttach(ctx, id, types.ExecStartCheck{
		Tty: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec process: %w", err)
	}
	defer resp.Close()

	dst := bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, err = io.Copy(&dst, resp.Reader)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		result := fmt.Sprintf("temporary output: %s", dst.String())
		err = fmt.Errorf("timeout value is too low, use greater value if you need so: %w: %s", ctx.Err(), result)
	}
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to copy output: %w", err)
	}

	// wait for the exec process to finish
	_, err = t.dockerClient.ContainerExecInspect(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec process: %w", err)
	}

	results := dst.String()
	styledOutput := fmt.Sprintf("%s%s%s%s", ansiColorSystemMsg, results, ansiColorReset, ansiLineTerminator)
	_, err = t.tlp.PutMsg(ctx, database.TermlogTypeStdout, styledOutput, t.containerID)
	if err != nil {
		return "", fmt.Errorf("failed to put terminal log (stdout): %w", err)
	}

	if results == "" {
		results = "Process terminated with status 0"
	}

	return results, nil
}

func (t *terminal) ReadFile(ctx context.Context, flowID int64, path string) (string, error) {
	container := PrimaryTerminalName(flowID)

	isRunning, err := t.dockerClient.VerifyContainerRuntime(ctx, t.containerLID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	if !isRunning {
		return "", fmt.Errorf("container is not running")
	}

	cwd := docker.WorkFolderPathInContainer
	catCommand := fmt.Sprintf("cat %s", path)
	styledCommand := fmt.Sprintf("%s $ %s%s%s%s", cwd, ansiColorInputCmd, catCommand, ansiColorReset, ansiLineTerminator)
	_, err = t.tlp.PutMsg(ctx, database.TermlogTypeStdin, styledCommand, t.containerID)
	if err != nil {
		return "", fmt.Errorf("failed to put terminal log (read file cmd): %w", err)
	}

	reader, stats, err := t.dockerClient.CopyFromContainer(ctx, container, path)
	if err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}
	defer reader.Close()

	var buffer strings.Builder
	tarReader := tar.NewReader(reader)
	for {
		tarHeader, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar header: %w", err)
		}

		if tarHeader.FileInfo().IsDir() {
			continue
		}

		if stats.Mode.IsDir() {
			buffer.WriteString("--------------------------------------------------\n")
			buffer.WriteString(
				fmt.Sprintf("'%s' file content (with size %d bytes) keeps bellow:\n",
					tarHeader.Name, tarHeader.Size,
				),
			)
		}

		var fileContent = make([]byte, tarHeader.Size)
		_, err = tarReader.Read(fileContent)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read file '%s' content: %w", tarHeader.Name, err)
		}
		buffer.Write(fileContent)

		if stats.Mode.IsDir() {
			buffer.WriteString("\n\n")
		}
	}

	content := buffer.String()
	styledContent := fmt.Sprintf("%s%s%s%s", ansiColorSystemMsg, content, ansiColorReset, ansiLineTerminator)
	_, err = t.tlp.PutMsg(ctx, database.TermlogTypeStdout, styledContent, t.containerID)
	if err != nil {
		return "", fmt.Errorf("failed to put terminal log (read file content): %w", err)
	}

	return content, nil
}

func (t *terminal) WriteFile(ctx context.Context, flowID int64, content string, path string) (string, error) {
	container := PrimaryTerminalName(flowID)

	isRunning, err := t.dockerClient.VerifyContainerRuntime(ctx, t.containerLID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	if !isRunning {
		return "", fmt.Errorf("container is not running")
	}

	tarBuffer := &bytes.Buffer{}
	archiveWriter := tar.NewWriter(tarBuffer)
	filename := filepath.Base(path)
	fileDescriptor := &tar.Header{
		Name: filename,
		Mode: 0600,
		Size: int64(len(content)),
	}
	err = archiveWriter.WriteHeader(fileDescriptor)
	if err != nil {
		return "", fmt.Errorf("tar archive header generation failed: %w", err)
	}

	_, err = archiveWriter.Write([]byte(content))
	if err != nil {
		return "", fmt.Errorf("tar archive content serialization failed: %w", err)
	}

	dir := filepath.Dir(path)
	err = t.dockerClient.CopyToContainer(ctx, container, dir, tarBuffer, types.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	})
	if err != nil {
		return "", fmt.Errorf("container file transfer failed: %w", err)
	}

	successMsg := fmt.Sprintf("File successfully saved to %s", path)
	styledMsg := fmt.Sprintf("%s%s%s%s", ansiColorSystemMsg, successMsg, ansiColorReset, ansiLineTerminator)
	_, err = t.tlp.PutMsg(ctx, database.TermlogTypeStdin, styledMsg, t.containerID)
	if err != nil {
		return "", fmt.Errorf("failed to put terminal log (write file cmd): %w", err)
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
}

func PrimaryTerminalName(flowID int64) string {
	return fmt.Sprintf("pentagi-terminal-%d", flowID)
}
