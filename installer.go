// Package browserpm provides dependency and driver installation for Playwright.
package browserpm

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/playwright-community/playwright-go"
)

// Installer handles Playwright dependency installation
type Installer struct {
	config     *Config
	log        Logger
	installed  bool
	installErr error
	once       sync.Once
}

// NewInstaller creates a new installer instance
func NewInstaller(cfg *Config, log Logger) *Installer {
	if log == nil {
		log = NewNopLogger()
	}
	return &Installer{
		config: cfg,
		log:    log,
	}
}

// Install executes the installation process (idempotent, thread-safe)
// playwright install itself is idempotent - it will update to latest if needed
func (i *Installer) Install() error {
	i.once.Do(func() {
		i.log.Info("Starting Playwright installation")

		// Create driver options
		options := &playwright.RunOptions{
			SkipInstallBrowsers: false,
			Browsers:            []string{"chromium"},
			Verbose:             true,
			Stdout:              io.Writer(os.Stdout),
			Stderr:              io.Writer(os.Stderr),
			DriverDirectory:     defaultInstallPath,
		}

		// Create driver instance
		driver, err := playwright.NewDriver(options)
		if err != nil {
			i.installErr = fmt.Errorf("failed to create driver: %w", err)
			i.log.Error("Failed to create driver", err)
			return
		}

		// Download driver
		i.log.Info("Downloading Playwright driver")
		if err := driver.DownloadDriver(); err != nil {
			i.installErr = fmt.Errorf("failed to download driver: %w", err)
			i.log.Error("Failed to download driver", err)
			return
		}

		// Build install command args
		args := []string{"install", "chromium"}
		if i.config.Install.WithDeps {
			args = append(args, "--with-deps")
		}

		// Install browser
		i.log.Info("Installing Chromium browser")
		cmd := driver.Command(args...)
		cmd.Stdout = options.Stdout
		cmd.Stderr = options.Stderr

		if err := cmd.Run(); err != nil {
			i.installErr = fmt.Errorf("failed to install browser: %w", err)
			i.log.Error("Failed to install browser", err)
			return
		}

		i.installed = true
		i.log.Info("Playwright installation completed successfully")
	})

	return i.installErr
}

// IsInstalled returns whether dependencies are installed
func (i *Installer) IsInstalled() bool {
	return i.installed
}

// EnsureInstalled checks and installs if necessary
func EnsureInstalled(cfg *Config, log Logger) error {
	installer := NewInstaller(cfg, log)
	return installer.Install()
}
