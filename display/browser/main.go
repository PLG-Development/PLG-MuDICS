package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chromedp/chromedp"
)

var Browser BrowserType = BrowserType{}

type BrowserType struct {
	Ctx    context.Context
	Cancel context.CancelFunc
}

func (b *BrowserType) Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to determine user home directory: %w", err)
	}
	browserProfileDirPath := filepath.Join(home, ".local", "share", "plg-mudics", "browser-display")
	if err := os.MkdirAll(browserProfileDirPath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create local config directory: %w", err)
	}

	opts := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", false),
		chromedp.Flag("app", "http://example.com"), // app mode prevents a few unwanted features of chrome, the start url is directly overwritten
		chromedp.Flag("start-fullscreen", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("allow-file-access-from-files", true),
		chromedp.Flag("user-data-dir", browserProfileDirPath),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
	}

	initCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	b.Ctx, b.Cancel = chromedp.NewContext(initCtx)

	return nil
}

func (b *BrowserType) OpenPage(url string) error {
	err := chromedp.Run(b.Ctx, chromedp.Navigate(url))
	if err != nil {
		return fmt.Errorf("navigate to URL: %w", err)
	}
	return nil
}

// Yes, we need that trick with creating a temp file and not directly sending html since
// chrome only allows us to access local files via other local files
func (b *BrowserType) OpenHTML(html string) error {
	var err error

	tempFile, err := os.CreateTemp("", "mudics-*.html")
	if err != nil {
		return fmt.Errorf("could not create tempfile: %w", err)
	}
	defer func() { _ = tempFile.Close() }()

	_, err = tempFile.WriteString(html)
	if err != nil {
		return fmt.Errorf("could not write to tempfile: %w", err)
	}

	err = chromedp.Run(b.Ctx, chromedp.Navigate("file://"+tempFile.Name()))
	if err != nil {
		return fmt.Errorf("navigate to URL: %w", err)
	}

	return nil
}

func (b *BrowserType) OpenPDF(path string) error {
	err := b.OpenPage("file://" + path + "#toolbar=0&view=Fit")
	if err != nil {
		return fmt.Errorf("open PDF: %w", err)
	}
	return nil
}
