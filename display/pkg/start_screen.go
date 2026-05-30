package pkg

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"net"
	"os"
	"plg-mudics/shared"
	"strings"

	"plg-mudics/display/browser"

	"github.com/skip2/go-qrcode"
)

func OpenStartScreen() error {
	var err error

	raw := shared.RawSplashScreenTemplate
	html := strings.ReplaceAll(raw, "%%APP-VERSION%%", shared.Version)

	ip, err := getDeviceIp()
	if err != nil {
		return fmt.Errorf("get device IP: %w", err)
	}
	mac, err := getDeviceMac()
	if err != nil {
		return fmt.Errorf("get device MAC address: %w", err)
	}

	port := 8080
	showQrCode := !isPortFree(port)
	qrCodePath := ""
	if showQrCode {
		qrCodePath, err = generateQRCode(fmt.Sprintf("http://%s:%d", ip, port))
		if err != nil {
			return fmt.Errorf("generate QR code: %w", err)
		}
	}

	var templateBuffer bytes.Buffer
	_ = startScreenTemplate(html, ip, mac, qrCodePath).Render(context.Background(), &templateBuffer)
	err = browser.Browser.OpenHTML(templateBuffer.String())
	if err != nil {
		return fmt.Errorf("open start screen in browser: %w", err)
	}
	return nil
}

func getDeviceIp() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("failed to get network interfaces: %w", err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no suitable IP address found")
}

func isPortFree(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func getDeviceMac() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, interf := range interfaces {
		mac := interf.HardwareAddr.String()
		if mac != "" {
			return mac, nil
		}
	}

	return "", fmt.Errorf("no suitable MAC address found")
}

func generateQRCode(data string) (string, error) {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("could not generate qr code: %w", err)
	}

	qr.DisableBorder = true
	qr.ForegroundColor = color.RGBA{R: 0x1c, G: 0x19, B: 0x17, A: 0xff}
	qr.BackgroundColor = color.RGBA{R: 0xe7, G: 0xe5, B: 0xe4, A: 0xff}

	png, err := qr.PNG(-1)
	if err != nil {
		return "", fmt.Errorf("could not render qr code: %w", err)
	}

	file, err := os.CreateTemp("", "mudics-qr-code-*.png")
	if err != nil {
		return "", fmt.Errorf("could not save qr code: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = file.Write(png)
	if err != nil {
		return "", fmt.Errorf("could not write qr code to file: %w", err)
	}

	return file.Name(), nil
}
