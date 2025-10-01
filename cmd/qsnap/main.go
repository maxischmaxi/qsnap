package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/maxischmaxi/qsnap/internal/browser"
	"github.com/maxischmaxi/qsnap/internal/config"
	"github.com/maxischmaxi/qsnap/internal/diff"
	"github.com/maxischmaxi/qsnap/internal/logging"
	"github.com/maxischmaxi/qsnap/internal/pool"
	"github.com/maxischmaxi/qsnap/internal/report"
	"github.com/maxischmaxi/qsnap/internal/snapshot"
	"github.com/maxischmaxi/qsnap/internal/storybook"
	"github.com/maxischmaxi/qsnap/internal/tools"
	"github.com/maxischmaxi/qsnap/internal/ui"
	"go.uber.org/zap"
)

func main() {
	var (
		input       = flag.String("input", ".", "the storybook directory you want to run snapshot tests in")
		concurrency = flag.Int("concurrency", 10, "number of concurrent screenshot tasks")
		instances   = flag.Int("instances", 4, "number of browser instances to use")
		timeoutSec  = flag.Int("timeout", 30, "timeout in seconds for each screenshot task")
		baseConfig  = flag.String("baseConfig", "osnap.config.yaml", "path to the base osnap config file")
		sbPort      = flag.Int("storybookPort", 3000, "the port where storybook is running (if empty, assumes storybook is already running)")
		sbBuildCmd  = flag.String("storybookBuildCmd", "npm run project:build:storybook", "the command to build storybook (only if -storybookPort is empty)")
		sbBuildDir  = flag.String("storybookBuildDir", "storybook-static", "the directory where the built storybook files are located (relative to -input)")
		sbForce     = flag.Bool("storybookForce", false, "force rebuild of storybook even if -storybookPort is set")
		sbWaitSec   = flag.Int("storybookWaitSec", 60, "how many seconds to wait for storybook to become available")
		sbHealth    = flag.String("sb-health-path", "/index.html", "the HTTP path to check for storybook health")
		logFile     = flag.String("logFile", "", "if set, log to this file instead of stdout")
		logLevel    = flag.String("logLevel", "error", "log level: debug, info, warn, error")
		logJSON     = flag.Bool("logJSON", false, "if true, log in JSON format")
		logConsole  = flag.Bool("logConsole", true, "if true, log to console")
		logDev      = flag.Bool("logDev", false, "if true, use a more human friendly console log format")
	)

	flag.Parse()

	cleanupLogs, err := logging.Init(logging.Config{
		Level:       *logLevel,
		FilePath:    *logFile,
		MaxSizeMB:   1000,
		MaxBackups:  5,
		MaxAgeDays:  14,
		JSON:        *logJSON,
		Console:     *logConsole,
		Development: *logDev,
	})
	if err != nil {
		logging.L.Fatal("failed to initialize logging", zap.Error(err))
		os.Exit(1)
	}
	defer cleanupLogs()

	logging.L.Info("qsnap started",
		zap.String("input", *input),
		zap.Int("concurrency", *concurrency),
		zap.Int("instances", *instances),
		zap.Int("timeoutSec", *timeoutSec),
		zap.String("baseConfig", *baseConfig),
		zap.Int("sbPort", *sbPort),
		zap.String("sbBuildCmd", *sbBuildCmd),
		zap.String("sbBuildDir", *sbBuildDir),
		zap.Bool("sbForce", *sbForce),
		zap.Int("sbWaitSec", *sbWaitSec),
		zap.String("sbHealth", *sbHealth),
		zap.String("logFile", *logFile),
		zap.String("logLevel", *logLevel),
		zap.Bool("logJSON", *logJSON),
		zap.Bool("logConsole", *logConsole),
		zap.Bool("logDev", *logDev),
	)

	baseDir, err := tools.ExpandPath(*input)
	if err != nil {
		logging.L.Fatal("failed to expand input path", zap.String("input", *input), zap.Error(err))
		os.Exit(1)
	}

	cfg, err := config.NewOsnapBaseConfig(filepath.Join(baseDir, *baseConfig))
	if err != nil {
		logging.L.Fatal("failed to load base config", zap.String("file", *baseConfig), zap.Error(err))
		os.Exit(1)
	}

	configs, err := cfg.FindAndParseConfigs(*input)
	if err != nil {
		logging.L.Fatal("failed to find/parse config files", zap.String("dir", *input), zap.Error(err))
		os.Exit(1)
	}

	err = os.MkdirAll(cfg.SnapshotDirectory, 0o755)
	if err != nil {
		logging.L.Fatal("failed to create snapshot directory", zap.String("dir", cfg.SnapshotDirectory), zap.Error(err))
		os.Exit(1)
	}

	logging.L.Info("loaded base config", zap.String("file", *baseConfig), zap.Any("config", cfg))

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	if err := storybook.BuildIfNeeded(rootCtx, *sbBuildCmd, filepath.Join(baseDir, *sbBuildDir), baseDir, *sbForce); err != nil {
		logging.L.Fatal("failed to build storybook", zap.Error(err))
		os.Exit(1)
	}

	logging.L.Info("serving storybook", zap.Int("port", *sbPort), zap.String("dir", filepath.Join(baseDir, *sbBuildDir)))

	ctrl, started, err := storybook.ServeBuildIfNeeded(
		rootCtx,
		*sbPort,
		filepath.Join(baseDir, *sbBuildDir),
		*sbHealth,
		time.Duration(*sbWaitSec)*time.Second,
		"",
	)
	if err != nil {
		logging.L.Fatal("failed to serve storybook", zap.Error(err))
		os.Exit(1)
	}
	defer func() {
		if ctrl != nil {
			ctrl.Stop()
		}
	}()

	if started {
		logging.L.Info("storybook server started", zap.Int("port", *sbPort))
	} else {
		logging.L.Info("storybook server already running", zap.Int("port", *sbPort))
	}

	instancesClamped := max(*instances, 1)

	brs, err := browser.LaunchPool(rootCtx, instancesClamped)
	if err != nil {
		logging.L.Fatal("failed to launch browser instances", zap.Error(err))
		os.Exit(1)
	}
	defer brs.CloseAll()

	wp := pool.New(*concurrency)
	results := make([]report.CaseResult, len(configs))
	waitSelList := snapshot.ParseSelectors("#storybook-root, #root")

	sendUI, stopUI := ui.Run(rootCtx, len(configs))
	defer stopUI()

	for i, s := range configs {
		i, s := i, s // capture loop variables

		wp.Go(func() {
			b := brs.Pick()
			instID := b.ID

			sendUI(ui.Event{
				Type:     ui.EvtStart,
				Name:     s.Name,
				URL:      s.URL,
				Instance: instID,
			})

			ctx, cancel := context.WithTimeout(rootCtx, time.Duration(*timeoutSec)*time.Second)
			defer cancel()

			filename := fmt.Sprintf("%s_%dx%d.png", s.Name, s.Width, s.Height)

			pngPath := filepath.Join(baseDir, "..", "__image-snapshots__", "__diff__", filename)
			baselinePath := filepath.Join(baseDir, "..", "__image-snapshots__", "__base_images__", filename)

			url := fmt.Sprintf("http://127.0.0.1:%d%s", *sbPort, s.URL)
			logging.L.Info("capturing snapshot", zap.String("name", s.Name), zap.String("url", url), zap.String("file", pngPath), zap.String("instance", fmt.Sprintf("%d", instID)), zap.Int("width", s.Width), zap.Int("height", s.Height))

			_, err := snapshot.Capture(ctx, b, url, pngPath, s.Width, s.Height, waitSelList)
			if err != nil {
				results[i] = report.CaseResult{
					Name:     s.Name,
					URL:      s.URL,
					Status:   "error",
					Error:    err.Error(),
					OutPath:  pngPath,
					Baseline: baselinePath,
				}
				sendUI(ui.Event{
					Type:     ui.EvtDone,
					Name:     s.Name,
					URL:      s.URL,
					Instance: instID,
					Status:   "error",
					Error:    err.Error(),
				})
				return
			}

			threshold := cfg.Threshold
			if s.Threshold != nil {
				threshold = *s.Threshold
			}

			df, ph, err := diff.CompareFiles(baselinePath, pngPath, float64(threshold), 10)
			if err != nil {
				results[i] = report.CaseResult{
					Name:     s.Name,
					URL:      s.URL,
					Status:   "error",
					Error:    err.Error(),
					OutPath:  pngPath,
					Baseline: baselinePath,
				}
				sendUI(ui.Event{
					Type:     ui.EvtDone,
					Name:     s.Name,
					URL:      s.URL,
					Instance: instID,
					Status:   "error",
					Error:    err.Error(),
				})
				return
			}

			status := "pass"
			if os.IsNotExist(err) {
				status = "no-baseline"
			} else if !df.Pass {
				status = "fail"
			}

			results[i] = report.CaseResult{
				Name:       s.Name,
				URL:        s.URL,
				Status:     status,
				Error:      "",
				OutPath:    pngPath,
				Baseline:   baselinePath,
				PixelDiff:  df,
				PercepDiff: ph,
			}

			sendUI(ui.Event{
				Type:     ui.EvtDone,
				Name:     s.Name,
				URL:      s.URL,
				Instance: instID,
				Status:   status,
			})
		})
	}

	wp.Wait()

	rep := report.Report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Total:       len(results),
		Passed:      report.CountStatus(results, "pass"),
		Failed:      report.CountStatus(results, "fail"),
		NoBaseline:  report.CountStatus(results, "no-baseline"),
		Errored:     report.CountStatus(results, "error"),
		Cases:       results,
	}

	reportPath := filepath.Join(baseDir, "report.json")
	err = report.Write(reportPath, rep)
	if err != nil {
		logging.L.Fatal("failed to write report", zap.String("file", reportPath), zap.Error(err))
		os.Exit(1)
	}
	logging.L.Info("wrote report", zap.String("file", reportPath))
}
