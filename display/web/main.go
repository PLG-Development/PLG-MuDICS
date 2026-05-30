package web

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	shared "plg-mudics/shared"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"plg-mudics/display/browser"
	"plg-mudics/display/pkg"
)

func StartWebServer(port string) {
	e := echo.New()

	apiGroup := e.Group("/api")
	apiGroup.Use(middleware.CORS())
	apiGroup.GET("/ping", pingRoute)
	apiGroup.PATCH("/shellCommand", shellCommandRoute)
	apiGroup.PATCH("/keyboardInput", keyboardInputRoute)
	apiGroup.PATCH("/showHTML", showHTMLRoute)
	apiGroup.PATCH("/takeScreenshot", takeScreenshotRoute)
	apiGroup.PATCH("/openWebsite", openWebsiteRoute)

	fileGroup := apiGroup.Group("/file")
	fileGroup.Use(extractFilePathMiddleware)
	fileGroup.POST("/:path", uploadFileRoute)
	fileGroup.GET("/:path", downloadFileRoute)
	fileGroup.PATCH("/:path", openFileRoute)
	fileGroup.GET("/preview/:path", previewRoute)

	err := e.Start(":" + port)
	if err != nil {
		slog.Error("Failed to start server", "error", err)
	}
}

func extractFilePathMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		raw := ctx.Param("path")

		decoded, err := url.PathUnescape(raw)
		if err != nil {
			slog.Warn("Invalid path encoding", "path", raw, "error", err)
			return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: "Invalid file path"})
		}

		fullPath, exists, err := pkg.ResolveStorageFilePath(decoded)
		if err != nil {
			slog.Warn("Failed to validate file path", "path", decoded, "error", err)
			return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: "Invalid file path"})
		}

		ctx.Set("fullPath", fullPath)
		ctx.Set("fileExists", exists)
		return next(ctx)
	}
}

func shellCommandRoute(ctx echo.Context) error {
	var commandInput struct {
		Command string `json:"command"`
	}
	if err := ctx.Bind(&commandInput); err != nil {
		slog.Error("Failed to parse shell command", "error", err)
		return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: shared.BadRequestDescription})
	}

	cmd := exec.Command("bash", "-c", commandInput.Command)
	storagePath, err := pkg.GetStoragePath()
	if err != nil {
		slog.Error("Failed to get storage path", "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to get storage path"})
	}
	cmd.Dir = storagePath

	commandOutput := shared.RunShellCommand(cmd)
	if commandOutput.ExitCode != 0 {
		slog.Error("Shell command execution error", "error", commandOutput.Stderr)
	}

	slog.Info("Shell command executed successfully", "command", commandInput.Command, "exitCode", commandOutput.ExitCode)
	return ctx.JSON(http.StatusOK, commandOutput)
}

func keyboardInputRoute(ctx echo.Context) error {
	var request struct {
		Inputs []struct {
			Key    string `json:"key"`
			Action string `json:"action"`
		} `json:"inputs"`
	}
	if err := ctx.Bind(&request); err != nil {
		slog.Error("Failed to parse keyboard input", "error", err)
		return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: shared.BadRequestDescription})
	}

	var inputs []pkg.Input

	for _, input := range request.Inputs {
		if input.Action != "press" && input.Action != "release" {
			slog.Error("Invalid keyboard action", "action", input.Action)
			return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: fmt.Sprintf("Invalid action: %s", input.Action)})
		}

		var action pkg.KeyAction
		if input.Action == "press" {
			action = pkg.KeyPress
		}
		if input.Action == "release" {
			action = pkg.KeyRelease
		}

		inputs = append(inputs, pkg.Input{
			Key:    input.Key,
			Action: action,
		})
	}

	err := pkg.KeyboardInput(inputs)
	if err != nil {
		slog.Error("Failed to send keyboard input", "inputs", inputs, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to send keyboard input"})
	}

	slog.Info("Keyboard input sent")
	return ctx.JSON(http.StatusOK, struct{}{})
}

func uploadFileRoute(ctx echo.Context) error {
	var err error

	fullPath := ctx.Get("fullPath").(string)

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		slog.Error("Failed to create storage path", "error", err, "path", fullPath)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to prepare storage directory"})
	}

	if ctx.Get("fileExists").(bool) {
		return ctx.JSON(http.StatusConflict, shared.ErrorResponse{Description: "File already exists"})
	}

	file, err := os.Create(fullPath)
	if err != nil {
		slog.Error("Failed to create file", "file", fullPath, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to create file"})
	}
	defer func() {
		fileCloseErr := file.Close()
		if fileCloseErr != nil {
			slog.Error("Failed to close file", "file", fullPath, "error", fileCloseErr)
		}
		if err != nil {
			err = os.Remove(fullPath)
			if err != nil {
				slog.Warn("could not remove broken file", "file", fullPath, "error", err)
			}
		}
	}()

	_, err = io.Copy(file, ctx.Request().Body)
	if err != nil {
		slog.Error("Failed to write file", "file", fullPath, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to write file"})
	}

	err = file.Sync() // ensure data is flushed to disk
	if err != nil {
		slog.Error("Failed to sync file to disk", "file", fullPath, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to save file"})
	}

	slog.Info("File uploaded successfully", "path", fullPath)
	return ctx.JSON(http.StatusOK, struct{}{})
}

func downloadFileRoute(ctx echo.Context) error {
	fullPath := ctx.Get("fullPath").(string)

	if !ctx.Get("fileExists").(bool) {
		return ctx.JSON(http.StatusNotFound, shared.ErrorResponse{Description: "File not found"})
	}

	slog.Info("Serving file for download", "path", fullPath)

	return ctx.File(fullPath)
}

func openFileRoute(ctx echo.Context) error {
	var err error

	pathParam := ctx.Param("path")
	fullPath := ctx.Get("fullPath").(string)

	if !ctx.Get("fileExists").(bool) {
		return ctx.JSON(http.StatusNotFound, shared.ErrorResponse{Description: "File not found"})
	}

	err = pkg.OpenFile(fullPath)
	if err != nil {
		slog.Error("Failed to open file", "file", pathParam, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to open file"})
	}

	slog.Info("Successfully run file", "file", pathParam)
	return ctx.JSON(http.StatusOK, struct{}{})
}

func showHTMLRoute(ctx echo.Context) error {
	var request struct {
		HTML string `json:"html"`
	}
	if err := ctx.Bind(&request); err != nil {
		slog.Error("Failed to parse request", "error", err)
		return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: shared.BadRequestDescription})
	}

	err := pkg.ShowHTML(request.HTML)
	if err != nil {
		slog.Error("Failed to open html", "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to open html"})
	}

	slog.Info("HTML content sent to client")
	return ctx.JSON(http.StatusOK, struct{}{})
}

func pingRoute(ctx echo.Context) error {
	return ctx.JSON(http.StatusOK, struct {
		Version string `json:"version"`
	}{Version: shared.Version})
}

func takeScreenshotRoute(ctx echo.Context) error {
	var err error

	screenshotPath, err := pkg.TakeScreenshot()
	if err != nil {
		slog.Error("Failed to take screenshot", "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to take screenshot"})
	}

	err = ctx.File(screenshotPath)
	if err != nil {
		slog.Error("Failed to serve file", "file", screenshotPath, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to serve file"})
	}

	return nil
}

func previewRoute(ctx echo.Context) error {
	fullPath := ctx.Get("fullPath").(string)
	exists := ctx.Get("fileExists").(bool)
	if !exists {
		return ctx.JSON(http.StatusNotFound, shared.ErrorResponse{Description: "File not found"})
	}

	outputFilePath, err := pkg.GenerateFilePreview(fullPath)
	if err != nil {
		slog.Error("Failed to generate preview", "file", fullPath, "error", err)
		if errors.Is(err, pkg.ErrFileTypePreviewNotSupported) {
			return ctx.JSON(http.StatusUnsupportedMediaType, shared.ErrorResponse{Description: "File type not supported for preview"})
		}
		if errors.Is(err, pkg.ErrFilePreviewToolsMissing) {
			return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Required tools for file preview are missing"})
		}
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to generate preview"})
	}

	return ctx.File(outputFilePath)
}

func openWebsiteRoute(ctx echo.Context) error {
	var err error

	var request struct {
		URL string `json:"url"`
	}
	if err := ctx.Bind(&request); err != nil {
		slog.Error("Failed to parse website input", "error", err)
		return ctx.JSON(http.StatusBadRequest, shared.ErrorResponse{Description: shared.BadRequestDescription})
	}

	slog.Info("Opening url")

	err = browser.Browser.OpenPage(request.URL)
	if err != nil {
		slog.Error("Failed to open website", "url", request.URL, "error", err)
		return ctx.JSON(http.StatusInternalServerError, shared.ErrorResponse{Description: "Failed to open website"})
	}

	return ctx.JSON(http.StatusOK, struct{}{})
}
