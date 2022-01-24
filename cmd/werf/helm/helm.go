package helm

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/cmd/helm"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"

	"github.com/werf/kubedog/pkg/kube"
	"github.com/werf/werf/cmd/werf/common"
	helm_secret_decrypt "github.com/werf/werf/cmd/werf/helm/secret/decrypt"
	helm_secret_encrypt "github.com/werf/werf/cmd/werf/helm/secret/encrypt"
	helm_secret_file_decrypt "github.com/werf/werf/cmd/werf/helm/secret/file/decrypt"
	helm_secret_file_edit "github.com/werf/werf/cmd/werf/helm/secret/file/edit"
	helm_secret_file_encrypt "github.com/werf/werf/cmd/werf/helm/secret/file/encrypt"
	helm_secret_generate_secret_key "github.com/werf/werf/cmd/werf/helm/secret/generate_secret_key"
	helm_secret_rotate_secret_key "github.com/werf/werf/cmd/werf/helm/secret/rotate_secret_key"
	helm_secret_values_decrypt "github.com/werf/werf/cmd/werf/helm/secret/values/decrypt"
	helm_secret_values_edit "github.com/werf/werf/cmd/werf/helm/secret/values/edit"
	helm_secret_values_encrypt "github.com/werf/werf/cmd/werf/helm/secret/values/encrypt"
	"github.com/werf/werf/pkg/deploy/helm"
	"github.com/werf/werf/pkg/deploy/helm/chart_extender"
	"github.com/werf/werf/pkg/deploy/helm/chart_extender/helpers"
	"github.com/werf/werf/pkg/werf"
)

var _commonCmdData common.CmdData

func NewCmd() *cobra.Command {
	var namespace string
	actionConfig := new(action.Configuration)

	cmd := &cobra.Command{
		Use:   "helm",
		Short: "Manage application deployment with helm",
	}

	ctx := common.GetContext()

	wc := chart_extender.NewWerfChartStub(ctx)

	loader.GlobalLoadOptions = &loader.LoadOptions{
		ChartExtender:               wc,
		SubchartExtenderFactoryFunc: func() chart.ChartExtender { return chart_extender.NewWerfChartStub(ctx) },
	}

	os.Setenv("HELM_EXPERIMENTAL_OCI", "1")

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", *helm_v3.Settings.GetNamespaceP(), "namespace scope for this request")
	common.SetupTmpDir(&_commonCmdData, cmd)
	common.SetupHomeDir(&_commonCmdData, cmd)
	common.SetupKubeConfig(&_commonCmdData, cmd)
	common.SetupKubeConfigBase64(&_commonCmdData, cmd)
	common.SetupKubeContext(&_commonCmdData, cmd)
	common.SetupStatusProgressPeriod(&_commonCmdData, cmd)
	common.SetupHooksStatusProgressPeriod(&_commonCmdData, cmd)
	common.SetupReleasesHistoryMax(&_commonCmdData, cmd)
	common.SetupLogOptions(&_commonCmdData, cmd)
	common.SetupInsecureHelmDependencies(&_commonCmdData, cmd)

	cmd.AddCommand(
		helm_v3.NewUninstallCmd(actionConfig, os.Stdout, helm_v3.UninstallCmdOptions{}),
		helm_v3.NewDependencyCmd(actionConfig, os.Stdout),
		helm_v3.NewGetCmd(actionConfig, os.Stdout),
		helm_v3.NewHistoryCmd(actionConfig, os.Stdout),
		NewLintCmd(actionConfig, wc),
		helm_v3.NewListCmd(actionConfig, os.Stdout),
		NewTemplateCmd(actionConfig, wc),
		helm_v3.NewRepoCmd(os.Stdout),
		helm_v3.NewRollbackCmd(actionConfig, os.Stdout),
		NewInstallCmd(actionConfig, wc),
		NewUpgradeCmd(actionConfig, wc),
		helm_v3.NewCreateCmd(os.Stdout),
		helm_v3.NewEnvCmd(os.Stdout),
		helm_v3.NewPackageCmd(os.Stdout),
		helm_v3.NewPluginCmd(os.Stdout),
		helm_v3.NewPullCmd(actionConfig, os.Stdout),
		helm_v3.NewSearchCmd(os.Stdout),
		helm_v3.NewShowCmd(os.Stdout),
		helm_v3.NewStatusCmd(actionConfig, os.Stdout),
		helm_v3.NewTestCmd(actionConfig, os.Stdout),
		helm_v3.NewVerifyCmd(os.Stdout),
		helm_v3.NewVersionCmd(os.Stdout),
		secretCmd(),
		NewGetAutogeneratedValuesCmd(),
		NewGetNamespaceCmd(),
		NewGetReleaseCmd(),
		NewMigrate2To3Cmd(),
		helm_v3.NewRegistryCmd(actionConfig, os.Stdout),
	)

	helm_v3.LoadPlugins(cmd, os.Stdout)

	commandsQueue := []*cobra.Command{cmd}
	for len(commandsQueue) > 0 {
		cmd := commandsQueue[0]
		commandsQueue = commandsQueue[1:]

		commandsQueue = append(commandsQueue, cmd.Commands()...)

		if cmd.Runnable() {
			oldRunE := cmd.RunE
			oldRun := cmd.Run
			cmd.RunE = func(cmd *cobra.Command, args []string) error {
				// NOTE: Common init block for all runnable commands.

				if err := werf.Init(*_commonCmdData.TmpDir, *_commonCmdData.HomeDir); err != nil {
					return err
				}

				if err := common.ProcessLogOptions(&_commonCmdData); err != nil {
					common.PrintHelp(cmd)
					return err
				}

				// FIXME: setup namespace env var for helm diff plugin
				os.Setenv("WERF_HELM3_MODE", "1")

				ctx := common.GetContext()

				stubCommitDate := time.Unix(0, 0)

				if vals, err := helpers.GetServiceValues(ctx, "PROJECT", "REPO", nil, helpers.ServiceValuesOptions{
					Namespace:  namespace,
					IsStub:     true,
					CommitHash: "COMMIT_HASH",
					CommitDate: &stubCommitDate,
				}); err != nil {
					return fmt.Errorf("error creating service values: %s", err)
				} else {
					wc.SetStubServiceValues(vals)
				}

				common.SetupOndemandKubeInitializer(*_commonCmdData.KubeContext, *_commonCmdData.KubeConfig, *_commonCmdData.KubeConfigBase64, *_commonCmdData.KubeConfigPathMergeList)

				helmRegistryClientHandle, err := common.NewHelmRegistryClientHandle(ctx, &_commonCmdData)
				if err != nil {
					return fmt.Errorf("unable to create helm registry client: %s", err)
				}

				helm.InitActionConfig(ctx, common.GetOndemandKubeInitializer(), namespace, helm_v3.Settings, helmRegistryClientHandle, actionConfig, helm.InitActionConfigOptions{
					StatusProgressPeriod:      time.Duration(*_commonCmdData.StatusProgressPeriodSeconds) * time.Second,
					HooksStatusProgressPeriod: time.Duration(*_commonCmdData.HooksStatusProgressPeriodSeconds) * time.Second,
					KubeConfigOptions: kube.KubeConfigOptions{
						Context:          *_commonCmdData.KubeContext,
						ConfigPath:       *_commonCmdData.KubeConfig,
						ConfigDataBase64: *_commonCmdData.KubeConfigBase64,
					},
					ReleasesHistoryMax: *_commonCmdData.ReleasesHistoryMax,
				})

				if oldRun != nil {
					oldRun(cmd, args)
					return nil
				} else {
					if err := oldRunE(cmd, args); err != nil {
						errValue := reflect.ValueOf(err)
						if errValue.Kind() == reflect.Struct {
							if !errValue.IsZero() {
								codeValue := errValue.FieldByName("code")
								if codeValue.IsValid() && !codeValue.IsZero() {
									os.Exit(int(codeValue.Int()))
								}
							}
						}

						return err
					}

					return nil
				}
			}
		}
	}

	return cmd
}

func secretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Work with secrets",
	}

	fileCmd := &cobra.Command{
		Use:   "file",
		Short: "Work with secret files",
	}

	fileCmd.AddCommand(
		helm_secret_file_encrypt.NewCmd(),
		helm_secret_file_decrypt.NewCmd(),
		helm_secret_file_edit.NewCmd(),
	)

	valuesCmd := &cobra.Command{
		Use:   "values",
		Short: "Work with secret values files",
	}

	valuesCmd.AddCommand(
		helm_secret_values_encrypt.NewCmd(),
		helm_secret_values_decrypt.NewCmd(),
		helm_secret_values_edit.NewCmd(),
	)

	cmd.AddCommand(
		fileCmd,
		valuesCmd,
		helm_secret_generate_secret_key.NewCmd(),
		helm_secret_encrypt.NewCmd(),
		helm_secret_decrypt.NewCmd(),
		helm_secret_rotate_secret_key.NewCmd(),
	)

	return cmd
}
