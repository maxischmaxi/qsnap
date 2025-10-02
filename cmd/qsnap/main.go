package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maxischmaxi/qsnap/internal/browser"
	"github.com/maxischmaxi/qsnap/internal/config"
	"github.com/maxischmaxi/qsnap/internal/diff"
	"github.com/maxischmaxi/qsnap/internal/pool"
	"github.com/maxischmaxi/qsnap/internal/report"
	"github.com/maxischmaxi/qsnap/internal/snapshot"
	"github.com/maxischmaxi/qsnap/internal/storybook"
	"github.com/maxischmaxi/qsnap/internal/tools"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Printf("num of cpu cores: %d\n", runtime.NumCPU())

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
		chromeArgs  = flag.String("chromeArgs", "", "additional comma-separated arguments to pass to Chrome instances")
		limit       = flag.Int("limit", 0, "if > 0, only process this many stories from the config files")
	)

	flag.Parse()

	chromeArgsList := []string{}
	if *chromeArgs != "" {
		chromeArgsList = strings.Split(*chromeArgs, ",")
	}
	if len(chromeArgsList) > 0 {
		fmt.Println("Using additional Chrome args:", chromeArgsList)
	}

	baseDir, err := tools.ExpandPath(*input)
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := config.NewOsnapBaseConfig(filepath.Join(baseDir, *baseConfig))
	if err != nil {
		log.Fatal(err)
	}

	configs, err := cfg.FindAndParseConfigs(*input)
	if err != nil {
		log.Fatal(err)
	}

	err = os.MkdirAll(cfg.SnapshotDirectory, 0o755)
	if err != nil {
		log.Fatal(err)
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	if err := storybook.BuildIfNeeded(rootCtx, *sbBuildCmd, filepath.Join(baseDir, *sbBuildDir), baseDir, *sbForce); err != nil {
		log.Fatal(err)
	}

	ctrl, started, err := storybook.ServeBuildIfNeeded(
		rootCtx,
		*sbPort,
		filepath.Join(baseDir, *sbBuildDir),
		*sbHealth,
		time.Duration(*sbWaitSec)*time.Second,
		"",
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if ctrl != nil {
			ctrl.Stop()
		}
	}()

	if started {
		fmt.Println("started storybook server on port", *sbPort)
	} else {
		fmt.Println("using existing storybook server on port", *sbPort)
	}

	instancesClamped := max(*instances, 1)

	brs, err := browser.LaunchPool(rootCtx, instancesClamped, chromeArgsList)
	if err != nil {
		log.Fatal(err)
	}
	defer brs.CloseAll()

	wp := pool.New(*concurrency)
	results := make([]report.CaseResult, len(configs))
	waitSelList := snapshot.ParseSelectors("#storybook-root, #root")

	configsToProcess := configs
	if *limit > 0 && *limit < len(configs) {
		configsToProcess = configs[:*limit]
		fmt.Println("limiting to first", *limit, "stories")
	}

	fmt.Println("Processing", len(configsToProcess), "stories")

	for i, s := range configsToProcess {
		i, s := i, s // capture loop variables

		wp.Go(func() {
			b := brs.Pick()

			ctx, cancel := context.WithTimeout(rootCtx, time.Duration(*timeoutSec)*time.Second)
			defer cancel()

			filename := fmt.Sprintf("%s_%dx%d.png", s.Name, s.Width, s.Height)

			diffPath := filepath.Join(baseDir, "..", "__image-snapshots__", "__diff__", filename)
			baselinePath := filepath.Join(baseDir, "..", "__image-snapshots__", "__base_images__", filename)

			url := fmt.Sprintf("http://127.0.0.1:%d%s", *sbPort, s.URL)

			buf, err := snapshot.Capture(ctx, b, url, diffPath, s.Width, s.Height, waitSelList)
			if err != nil {
				results[i] = report.CaseResult{
					Name:     s.Name,
					URL:      s.URL,
					Status:   "error",
					Error:    err.Error(),
					OutPath:  diffPath,
					Baseline: baselinePath,
				}
				return
			}

			threshold := cfg.Threshold
			if s.Threshold != nil {
				threshold = *s.Threshold
			}

			df, ph, err := diff.CompareFiles(baselinePath, buf, diffPath, float64(threshold), 10)
			if err != nil {
				results[i] = report.CaseResult{
					Name:     s.Name,
					URL:      s.URL,
					Status:   "error",
					Error:    err.Error(),
					OutPath:  diffPath,
					Baseline: baselinePath,
				}
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
				OutPath:    diffPath,
				Baseline:   baselinePath,
				PixelDiff:  df,
				PercepDiff: ph,
			}

			snapshotNumber := fmt.Sprintf("[%d]", i+1)
			fmt.Printf("%s %s - %s\n", snapshotNumber, s.Name, status)
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
		log.Fatal(err)
	}
	log.Println("wrote report to", reportPath)
}
