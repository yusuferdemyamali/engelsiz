package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"engelsiz/internal/api"
	"engelsiz/internal/config"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
	"engelsiz/internal/providers/kiwix"
	"engelsiz/internal/web"
)

const PreferredPort = 17321

type Options struct {
	NoBrowser       bool
	KiwixBinary     string
	ContentProvider providers.ContentProvider
}

func ListenLoopback(preferred int) (net.Listener, int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", preferred)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, ln.Addr().(*net.TCPAddr).Port, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, 0, fmt.Errorf("listen %s: %w", addr, err)
	}
	fallback, err2 := net.Listen("tcp", "127.0.0.1:0")
	if err2 != nil {
		return nil, 0, fmt.Errorf("preferred %d busy (%v) and fallback failed: %w", preferred, err, err2)
	}
	port := fallback.Addr().(*net.TCPAddr).Port
	return fallback, port, nil
}

func Run(ctx context.Context, opts Options) (runErr error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if ctx.Err() != nil {
		return nil
	}
	dirs, err := config.Resolve()
	if err != nil {
		return fmt.Errorf("resolve dirs: %w", err)
	}
	if err := dirs.Ensure(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}
	logger, logFile, err := initLogger(dirs.LogPath)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close log: %w", err))
		}
	}()
	logger.Printf("startup: config=%s data=%s cache=%s", dirs.ConfigRoot, dirs.DataRoot, dirs.CacheRoot)

	db, err := database.Open(dirs.DBPath)
	if err != nil {
		logger.Printf("database open failed: %v", err)
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Printf("db close: %v", err)
			runErr = errors.Join(runErr, fmt.Errorf("close database: %w", err))
		}
		logger.Printf("shutdown complete")
	}()
	provider := opts.ContentProvider
	if provider == nil {
		provider, err = kiwix.New(kiwix.Options{BinaryPath: opts.KiwixBinary, Logger: logger})
		if err != nil {
			logger.Printf("content provider initialization failed: %v", err)
			return fmt.Errorf("initialize content provider: %w", err)
		}
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := provider.Stop(cleanupCtx); err != nil {
			logger.Printf("content provider stop failed: %v", err)
			runErr = errors.Join(runErr, fmt.Errorf("stop content provider: %w", err))
		}
	}()
	refreshNotifications := make(chan struct{}, 1)
	packageService, err := packages.New(ctx, db, dirs.DataRoot, packages.Options{Logger: logger, OnInstalled: func() {
		select {
		case refreshNotifications <- struct{}{}:
		default:
		}
	}})
	if err != nil {
		logger.Printf("package initialization failed: code=storage_error cause=%T", err)
		return fmt.Errorf("initialize packages: %w", err)
	}
	// This defer is registered before downloads; package workers need their
	// completed roots and database until they have stopped and joined.
	defer func() {
		if err := packageService.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close packages: %w", err))
		}
	}()
	manager, err := download.New(db, dirs.DownloadDir, download.Options{Logger: logger})
	if err != nil {
		logger.Printf("download initialization failed: code=storage_error cause=%T", err)
		return fmt.Errorf("initialize downloads: %w", err)
	}
	defer func() {
		if err := packageService.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close packages: %w", err))
		}
		if err := manager.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close downloads: %w", err))
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	logger.Printf("database ready: %s", dirs.DBPath)
	activation := newActivation(ctx, packageService, provider, logger, refreshNotifications)
	defer activation.cancel()
	if err := activation.refresh(false); err != nil {
		if errors.Is(err, download.ErrStorage) {
			return fmt.Errorf("read installed content: %w", err)
		}
		if ctx.Err() != nil && cancellationOnly(err, ctx.Err()) {
			logger.Printf("shutdown: context done during content startup")
			return nil
		}
		logger.Printf("content provider startup failed: %v", err)
		if ctx.Err() != nil {
			return fmt.Errorf("start content provider: %w", err)
		}
	}

	activation.start()
	defer activation.close()

	ln, port, err := ListenLoopback(PreferredPort)
	if err != nil {
		logger.Printf("listen failed: %v", err)
		return err
	}
	logger.Printf("listening on http://127.0.0.1:%d", port)

	mux := api.NewMux()
	mux.HandleFunc("/api/catalog", api.CatalogHandler(provider, logger))
	mux.HandleFunc("/api/packages", api.PackagesHandler(packageService))
	mux.HandleFunc("/api/packages/", api.UpdatePackageHandler(packageService, manager, provider))
	updatesHandler := api.UpdatesHandler(packageService, manager, provider)
	mux.HandleFunc("/api/updates", updatesHandler)
	mux.HandleFunc("/api/updates/", updatesHandler)
	mux.HandleFunc("/api/search", api.SearchHandler(activation, packageService))
	mux.HandleFunc("/api/runtime", api.RuntimeHandler(activation, false))
	mux.HandleFunc("/api/runtime/retry", api.RuntimeHandler(activation, true))
	resolver, _ := provider.(providers.AcquisitionResolver)
	downloadsHandler := api.DownloadsHandler(manager, resolver, packageService)
	mux.HandleFunc("/api/downloads", downloadsHandler)
	mux.HandleFunc("/api/downloads/", downloadsHandler)
	mux.Handle("/", web.Handler())
	server := &http.Server{Handler: api.WithContent(mux, activation, logger), ReadHeaderTimeout: 5 * time.Second}
	defer func() {
		if err := shutdown(server, logger); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForHealth(ctx, baseURL+"/api/health", 10*time.Second); err != nil {
		logger.Printf("health self-check failed: %v", err)
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("health self-check: %w", err)
	}
	logger.Printf("health check ok: %s", baseURL)

	if ctx.Err() != nil {
		return nil
	}
	var browserResult <-chan error
	if !opts.NoBrowser {
		cmd, err := openBrowser(baseURL + "/")
		if err != nil {
			logger.Printf("start browser helper failed: %v", err)
		} else {
			logger.Printf("browser helper started: %s", baseURL)
			// xdg-open may exec the browser itself. Reap completed helpers while
			// running, but never kill or await the user's browser on shutdown.
			result := make(chan error, 1)
			browserResult = result
			go func() { result <- cmd.Wait() }()
		}
	} else {
		logger.Printf("browser skipped (--no-browser)")
	}
	fmt.Printf("Engelsiz running at %s\n", baseURL)

	for {
		select {
		case <-ctx.Done():
			logger.Printf("shutdown: context done")
			return nil
		case err := <-serverErr:
			if err != nil {
				logger.Printf("server error: %v", err)
			}
			return err
		case err := <-browserResult:
			browserResult = nil
			if err != nil {
				logger.Printf("browser helper exited: %v", err)
			}
		}
	}
}

// A cancelled startup can also include a cleanup failure. Only suppress the
// cancellation itself; other errors must remain visible even during shutdown.
func cancellationOnly(err, cause error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !cancellationOnly(child, cause) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return cancellationOnly(wrapped, cause)
	}
	return errors.Is(err, cause)
}

func shutdown(server *http.Server, logger *log.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("server shutdown: %v", err)
		return errors.Join(fmt.Errorf("server shutdown: %w", err), server.Close())
	}
	return nil
}

func waitForHealth(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == `{"status":"ok"}` {
				return ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func openBrowser(url string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd, cmd.Start()
}
