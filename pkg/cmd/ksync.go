package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	"arhat.dev/pkg/envhelper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"arhat.dev/pkg/confhelper"
	"arhat.dev/pkg/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"arhat.dev/ksync/pkg/conf"
	"arhat.dev/ksync/pkg/constant"
	"arhat.dev/ksync/pkg/controller"
)

func NewKsyncCmd() *cobra.Command {
	var (
		appCtx       context.Context
		configFile   string
		config       = new(conf.KsyncConfig)
		cliLogConfig = new(log.Config)
	)

	ksyncCmd := &cobra.Command{
		Use:           "ksync",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Use == "version" {
				return nil
			}

			var err error
			flags := cmd.Flags()
			if flags.Changed("config") {
				var configBytes []byte
				configBytes, err = ioutil.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file %s: %v", configFile, err)
				}

				if err = yaml.Unmarshal(configBytes, config); err != nil {
					return fmt.Errorf("failed to unmarshal config file %s: %v", configFile, err)
				}
			}

			logConfigSet := config.Ksync.Log
			if len(logConfigSet) > 0 {
				if flags.Changed("log.format") {
					logConfigSet[0].Format = cliLogConfig.Format
				}

				if flags.Changed("log.level") {
					logConfigSet[0].Level = cliLogConfig.Level
				}

				if flags.Changed("log.file") {
					logConfigSet[0].File = cliLogConfig.File
				}
			} else {
				logConfigSet = append(logConfigSet, *cliLogConfig)
			}

			if err = cmd.ParseFlags(os.Args); err != nil {
				return err
			}

			err = log.SetDefaultLogger(logConfigSet)
			if err != nil {
				return err
			}

			var exit context.CancelFunc
			appCtx, exit = context.WithCancel(context.Background())

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				exitCount := 0
				for sig := range sigCh {
					switch sig {
					case os.Interrupt, syscall.SIGTERM:
						exitCount++
						if exitCount == 1 {
							exit()
						} else {
							os.Exit(1)
						}
						//case syscall.SIGHUP:
						//	// force reload
					}
				}
			}()

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(appCtx, config)
		},
	}

	flags := ksyncCmd.PersistentFlags()
	// config file
	flags.StringVarP(&configFile, "config", "c",
		constant.DefaultKsyncConfigFile, "path to the ksync config file")
	flags.BoolVar(&config.Ksync.Namespaced, "namespaced", false,
		"watch deployed namespace only")
	flags.DurationVar(&config.Ksync.ReloadDelay, "reloadDelay",
		constant.DefaultWorkloadReloadDelay, "set delay before reloading a workload")
	flags.StringSliceVar(&config.Ksync.IgnoredNamespaces, "ignoredNamespaces",
		nil, "ignore these namespaces when namespaced is true")

	flags.AddFlagSet(confhelper.FlagsForControllerConfig("ksync", "", cliLogConfig, &config.Ksync.ControllerConfig))

	return ksyncCmd
}

func run(appCtx context.Context, config *conf.KsyncConfig) error {
	logger := log.Log.WithName("ksync")

	logger.I("creating kube client for initialization")
	kubeClient, _, err := config.Ksync.KubeClient.NewKubeClient(nil, false)
	if err != nil {
		return fmt.Errorf("failed to create kube client from kubeconfig: %w", err)
	}

	if err = config.Ksync.Metrics.RegisterIfEnabled(appCtx, logger); err != nil {
		return fmt.Errorf("failed to register metrics controller: %w", err)
	}

	if err = config.Ksync.Tracing.RegisterIfEnabled(appCtx, logger); err != nil {
		return fmt.Errorf("failed to register tracing controller: %w", err)
	}

	logger.I("creating controller")
	ctrl, err := controller.NewController(appCtx, config)
	if err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	evb := record.NewBroadcaster()
	watchEventLogging := evb.StartLogging(func(format string, args ...interface{}) {
		logger.I(fmt.Sprintf(format, args...), log.String("source", "event"))
	})
	watchEventRecording := evb.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(envhelper.ThisPodNS()),
	})
	defer func() {
		watchEventLogging.Stop()
		watchEventRecording.Stop()
	}()

	logger.V("creating leader elector")
	elector, err := config.Ksync.LeaderElection.CreateElector("ksync", kubeClient,
		evb.NewRecorder(scheme.Scheme, corev1.EventSource{
			Component: "ksync",
		}),
		//  elected
		func(ctx context.Context) {
			logger.I("starting controller")
			if err = ctrl.Start(); err != nil {
				logger.E("failed to start controller", log.Error(err))
				os.Exit(1)
			}
		},
		func() {
			logger.E("lost leader-election")
			os.Exit(1)
		},
		func(identity string) {

		})

	if err != nil {
		return fmt.Errorf("failed to create elector: %w", err)
	}

	logger.I("running leader election")
	elector.Run(appCtx)

	return fmt.Errorf("unreachable code")
}
