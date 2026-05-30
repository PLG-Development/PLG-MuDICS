package pkg

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"plg-mudics/shared"
	"syscall"

	"github.com/gabriel-vasile/mimetype"

	"plg-mudics/display/browser"
)

var fileHandler fileHandlerType = fileHandlerType{}

type fileHandlerType struct {
	runningProgram *exec.Cmd
}

func OpenFile(path string) error {
	ResetView()

	mType, err := mimetype.DetectFile(path)
	if err != nil {
		return fmt.Errorf("detect mime type of file %q: %w", path, err)
	}

	switch mType.String() {
	case "video/mp4":
		var templateBuffer bytes.Buffer
		_ = videoTemplate(path).Render(context.Background(), &templateBuffer)
		err = browser.Browser.OpenHTML(templateBuffer.String())
	case "image/jpeg", "image/png", "image/gif":
		var templateBuffer bytes.Buffer
		_ = imageTemplate(path).Render(context.Background(), &templateBuffer)
		err = browser.Browser.OpenHTML(templateBuffer.String())
	case "application/pdf":
		err = browser.Browser.OpenPDF(path)
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.presentation":
		err = fileHandler.openFileWithApp(path)
	default:
		return fmt.Errorf("unsupported file type: %s", mType.String())
	}

	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	return nil
}

func (fh *fileHandlerType) openFileWithApp(path string) error {
	var err error

	mType, err := mimetype.DetectFile(path)
	if err != nil {
		return fmt.Errorf("failed to detect mime type: %w", err)
	}

	switch mType.String() {
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.presentation":
		err = fh.openLibreoffice(path)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported file type: %s", mType.String())
	}

	fh.runningProgram.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	result := shared.RunShellCommandNonBlocking(fh.runningProgram)
	if result.ExitCode != 0 {
		return fmt.Errorf("could not open pdf: %s (%d)", result.Stderr, result.ExitCode)
	}

	return nil
}

func (fh *fileHandlerType) openLibreoffice(path string) error {
	// yes, we need this weird workaround to delete lock files since libreoffice
	// doesn't expose an option to ignore them or prevent their creation
	// the --view argument for some reason doesn't work with --show
	parent := filepath.Dir(path)
	cmd := exec.Command("find", parent, "-name", ".~lock*", "-type", "f", "-delete")
	result := shared.RunShellCommand(cmd)
	if result.ExitCode != 0 {
		slog.Warn("could not remove lock files", "path", parent, "stderr", result.Stderr, "exitCode", result.ExitCode)
	}

	tempDirPath, err := os.MkdirTemp("", "plg-mudics-program-profile-")
	if err != nil {
		return fmt.Errorf("failed to create temporary profile directory: %w", err)
	}
	fh.runningProgram = exec.Command("soffice", "--show", path, "--nologo", "--norestore", fmt.Sprintf("-env:UserInstallation=file://%s", tempDirPath))

	return nil
}

func (fh *fileHandlerType) closeRunningProgram() error {
	if fh.runningProgram == nil {
		return nil
	}
	err := syscall.Kill(-fh.runningProgram.Process.Pid, syscall.SIGTERM)
	fh.runningProgram = nil
	return err
}
