// main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/olekukonko/tablewriter"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/siderolink/pkg/events"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/stolos-cloud/stolos-bootstrap/internal/tui"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/gcp"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/github"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/helm"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/k8s"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/marshal"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/oauth"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/state"
	"github.com/stolos-cloud/stolos-bootstrap/pkg/talos"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var bootstrapInfos = &state.BootstrapInfo{}
var doRestoreProgress = false
var didReadBootstrapInfos = false

// var tui.Steps []tui.Step
var kubeconfig []byte
var saveState state.SaveState
var gcpConfig *gcp.Config
var githubConfig *github.Config
var oauthServer *oauth.Server
var githubToken *oauth2.Token
var gcpToken *oauth2.Token
var gcpEnabled = gcp.GCPClientId != "" && gcp.GCPClientSecret != ""
var gitHubEnabled = github.GithubClientId != "" && github.GithubClientSecret != ""
var gitHubUser *github.User
var gitHubAppManifestParams *github.AppManifestParams
var gitHubAppManifest *github.AppManifest

const _gigabyte = 1073741824

func main() {

	tui.RegisterDefaultFunc("GetOutboundIP", GetOutboundIP)

	_, err := os.Stat("bootstrap-state.json")

	if !(errors.Is(err, os.ErrNotExist)) {
		saveState = marshal.ReadStateFromJSON()
		bootstrapInfos = &saveState.BootstrapInfo
		didReadBootstrapInfos = true
		doRestoreProgress = true
	} else {
		saveState = state.SaveState{
			MachinesDisks: make(map[string]string),
			MachinesCache: state.Machines{
				Workers:       make(map[string][]byte),
				ControlPlanes: make(map[string][]byte),
			},
		}
	}

	_, err = os.Stat("bootstrap-config.json")
	if !(errors.Is(err, os.ErrNotExist)) {
		marshal.ReadBootstrapInfos("bootstrap-config.json", bootstrapInfos)
		didReadBootstrapInfos = true
	}

	if !didReadBootstrapInfos {
		bootstrapInfos = &state.BootstrapInfo{}
	}

	githubInfoStep := tui.Step{
		Name:        "GitHubInfo",
		Title:       "1) Enter GitHub Repository Information",
		Kind:        tui.StepForm,
		Fields:      tui.CreateFieldsForStruct[github.GitHubInfo](),
		IsDone:      true,
		AutoAdvance: didReadBootstrapInfos,
		OnExit: func(m *tui.Model, s *tui.Step) {
			if !didReadBootstrapInfos {
				githubInfo, err := tui.RetrieveStructFromFields[github.GitHubInfo](s.Fields)
				if err != nil {
				}
				bootstrapInfos.GitHubInfo = *githubInfo
			}
		},
	}

	githubAuthStep := tui.Step{
		Name:        "GitHubAuth",
		Title:       "1.1) Authenticate with GitHub",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunGitHubAuthStepInBackground,
		OnExit: func(m *tui.Model, s *tui.Step) {
			// TODO CHECK GH AUTH
			oauthServer.Stop(context.Background())
		},
	}

	githubRepoStep := tui.Step{
		Name:        "GitHubRepo",
		Title:       "1.2) Create GitHub Repository",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunGitHubRepoStepInBackground,
		//OnExit: //TODO validate,
	}

	githubAppStep := tui.Step{
		// TODO: Implement GitHub App Manifest Flow
		Name:        "GitHubApp",
		Title:       "1.3) Create GitHub App",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter: func(m *tui.Model, s *tui.Step) tea.Cmd {
			m.Logger.Infof("Starting GitHub App Manifest Flow...")
			go func() {
				listenAddr := "127.0.0.1:19999"
				remoteBaseUrl := "https://api." + bootstrapInfos.GitHubInfo.BaseDomain //URL when deployed
				webhookEndpoint := "/api/v1/github_webhook"                            //webhook endpoint
				callbacksEndpoint := "/api/v1/github_callback"                         // additional install callback for deployed URL

				gitHubUser, err = github.GetGitHubUser(context.Background(), bootstrapInfos.GitHubInfo.RepoOwner, *githubToken)

				if err != nil {
					m.Logger.Errorf("Failed to get GitHub User: %v", err)
					return
				}

				if gitHubUser.Type != "Organization" {
					m.Logger.Errorf("You must login as an organization!")
					return
				}

				gitHubAppManifestParams = github.CreateGitHubManifestParameters(remoteBaseUrl, webhookEndpoint, callbacksEndpoint, listenAddr)
				gitHubAppManifest, err = github.GitHubAppManifestFlow(context.Background(), listenAddr, m.Logger, gitHubAppManifestParams, *gitHubUser)
				if err != nil {
					m.Logger.Errorf("GitHub App Manifest Flow Error: %s", err.Error())
				}

				saveState.GitHubApp = *gitHubAppManifest
				//err = marshal.SaveStateToJSON(saveState)
				//if err != nil {
				//	m.Logger.Errorf("Failed to save state: %s", err.Error())
				//	return
				//}

				m.Logger.Successf("GitHub App was created successfuly! App name: %s, App ID: %d", gitHubAppManifest.Name, gitHubAppManifest.ID)
				s.IsDone = true
			}()
			return nil
		},
	}

	githubInstallAppStep := tui.Step{
		Name:        "GitHubInstallApp",
		Title:       "1.3) Install GitHub App",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter: func(m *tui.Model, s *tui.Step) tea.Cmd {

			m.Logger.Infof("Opening the github app install page...")

			go func() {
				listenAddr := "127.0.0.1:19999"
				postInstallResult, err := github.GitHubAppInstallFlow(context.Background(), listenAddr, gitHubUser, gitHubAppManifest, m.Logger)
				if err != nil {
					m.Logger.Errorf("GitHub App Install Flow Error: %s", err.Error())
					return
				}
				saveState.GitHubAppInstallResult = *postInstallResult
				s.IsDone = true
			}()

			return nil
		},
	}

	gcpInfoStep := tui.Step{
		Name:        "GCPInfo",
		Title:       "2) GCP Information",
		Kind:        tui.StepForm,
		Fields:      tui.CreateFieldsForStruct[state.GCPInfo](),
		IsDone:      true,
		AutoAdvance: didReadBootstrapInfos,
		OnEnter: func(m *tui.Model, s *tui.Step) tea.Cmd {
			// TODO
			return nil
		},
		OnExit: func(m *tui.Model, s *tui.Step) {
			if !didReadBootstrapInfos {
				gcpInfo, err := tui.RetrieveStructFromFields[state.GCPInfo](s.Fields)
				if err != nil {
				}
				bootstrapInfos.GCPInfo = *gcpInfo
			}
		},
	}

	gcpAuthStep := tui.Step{
		Name:        "GCPAuth",
		Title:       "2.1) GCP Authentication",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter: func(m *tui.Model, s *tui.Step) tea.Cmd {
			if !gcpEnabled {
				return nil
			}
			RunOAuthServerInBackround(m.Logger)
			go func() {
				gcpToken, err = oauthServer.Authenticate(context.Background(), "GCP")
				if err != nil {
					m.Logger.Errorf("Failed to authenticate with GCP: %v", err)
					s.IsDone = true // TODO handle fail
				}
				s.IsDone = true
			}()
			return nil
		},
		OnExit: func(m *tui.Model, s *tui.Step) {
			// TODO CHECK GH AUTH
			oauthServer.Stop(context.Background())
		},
	}

	gcpSAStep := tui.Step{
		Name:        "GCPServiceAccount",
		Title:       "2.2) GCP Service Account",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunGCPSAStepInBackground,
	}

	talosInfoStep := tui.Step{
		Name:        "TalosInfo",
		Title:       "3) Talos and Kubernetes Information",
		Kind:        tui.StepForm,
		Fields:      tui.CreateFieldsForStruct[state.TalosInfo](),
		IsDone:      true,
		AutoAdvance: didReadBootstrapInfos,
		OnEnter: func(m *tui.Model, s *tui.Step) tea.Cmd {
			if doRestoreProgress {
				m.Logger.Info("State file found, skipping talos info form")
				s.IsDone = true
				s.AutoAdvance = true
			}
			return nil
		},
		OnExit: func(m *tui.Model, s *tui.Step) {
			if !didReadBootstrapInfos {
				talosInfo, err := tui.RetrieveStructFromFields[state.TalosInfo](s.Fields)
				if err != nil {
				}
				bootstrapInfos.TalosInfo = *talosInfo
			}
		},
	}

	talosISOStep := tui.Step{
		Name:        "TalosISOStep",
		Title:       "3.1) Download Talos ISO",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunTalosISOStep,
	}

	waitforServersStep := tui.Step{
		Name:        "WaitServersStep",
		Title:       "3.2) Wait for servers",
		Kind:        tui.StepSpinner,
		IsDone:      true, // Set by server
		AutoAdvance: false,
		Body:        "Press enter when you see all servers below (min 4):\n",
		OnEnter:     RunWaitForServersStep,
		OnExit:      ExitWaitForServersStep,
	}

	clusterBootstrapStep := tui.Step{
		Name:        "ClusterBootstrap",
		Title:       "3.4) Bootstrap Kubernetes",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunClusterBootstrapStepInBackground,
	}

	deployArgoStep := tui.Step{
		Name:        "DeployArgo",
		Title:       "4.1) Deploy Argo",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunArgoStepInBackground,
	}

	deployPortalStep := tui.Step{
		Name:        "DeployPortal",
		Title:       "4.2) Deploy Portal",
		Kind:        tui.StepSpinner,
		IsDone:      false,
		AutoAdvance: true,
		OnEnter:     RunPortalStepInBackground,
	}

	// Skip all gcp steps if not enabled
	tui.DisableStep(&gcpInfoStep, !gcpEnabled)
	tui.DisableStep(&gcpAuthStep, !gcpEnabled)
	tui.DisableStep(&gcpSAStep, !gcpEnabled)

	// Skip all github steps if not enabled
	tui.DisableStep(&githubInfoStep, !gitHubEnabled)
	tui.DisableStep(&githubAuthStep, !gitHubEnabled)
	tui.DisableStep(&githubRepoStep, !gitHubEnabled)
	tui.DisableStep(&githubAppStep, !gitHubEnabled)

	// Attn. sa fait des copies ici !
	tui.Steps = []*tui.Step{
		&githubInfoStep,
		&githubAuthStep,
		&githubRepoStep,
		&githubAppStep,
		&githubInstallAppStep,
		&gcpInfoStep,
		&gcpAuthStep,
		&gcpSAStep,
		&talosInfoStep,
		&talosISOStep,
		&waitforServersStep,
		&clusterBootstrapStep,
		&deployArgoStep,
		&deployPortalStep,
	}

	f, _ := os.OpenFile("./stolos.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	defer f.Close()
	p, model := tui.NewWizard(tui.Steps, f)

	// Setup the OAuth providers and get feature enablement state
	oauthServer = SetupOAuthServer(model.Logger)
	if gcpEnabled {
		SetupGCP()
	}
	if gitHubEnabled {
		SetupGitHub()
	}

	// Run will block.
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
	}

}

func RunOAuthServerInBackround(logger *tui.UILogger) {
	go func() {
		ctx := context.Background()
		defer oauthServer.Stop(ctx)
		if err := oauthServer.Start(ctx); err != nil {
			logger.Errorf("skipping, oauth server start failed: %v", err)
			tui.Steps[1].IsDone = true
			return
		}
		// Keep the server running
		select {}
	}()
}

func SetupOAuthServer(logger *tui.UILogger) *oauth.Server {
	// Setup OAuth server
	server := oauth.NewServer("9999", logger)
	return server
}

func SetupGitHub() {
	if github.GithubClientId != "" && github.GithubClientSecret != "" {
		githubProvider := oauth.NewGitHubProvider(github.GithubClientId, github.GithubClientSecret)
		oauthServer.RegisterProvider(githubProvider)
	}
}

func SetupGCP() {
	if gcp.GCPClientId != "" && gcp.GCPClientSecret != "" {
		gcpProvider := oauth.NewGCPProvider(gcp.GCPClientId, gcp.GCPClientSecret)
		oauthServer.RegisterProvider(gcpProvider)
	}
}

func RunGCPSAStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {

	if !gcpEnabled {
		return nil
	}

	m.Logger.Infof("Read bootstrap infos from file, clusterName: %s", bootstrapInfos.TalosInfo.ClusterName)

	go func() {
		if gcp.GCPClientId != "" && gcp.GCPClientSecret != "" {
			// Create GCP service account
			var err error
			gcpConfig, err = gcp.CreateServiceAccountWithOAuth(
				context.Background(),
				bootstrapInfos.GCPInfo.GCPProjectID,
				bootstrapInfos.GCPInfo.GCPRegion,
				gcpToken,
				"stolos-platform-sa",
			)
			if err != nil {
				m.Logger.Errorf("Failed to create GCP service account: %v", err)
			} else {
				m.Logger.Success("GCP service account created successfully")
			}
		} else {
			m.Logger.Infof("GCP OAuth credentials not provided, skipping GCP service account creation")
		}

		s.IsDone = true
	}()
	return nil
}

func RunGitHubRepoStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {
	m.Logger.Infof("Creating github repo %s...", bootstrapInfos.GitHubInfo.RepoName)
	// Create GitHub client and initialize repository
	githubClient := github.NewClient(githubToken)
	githubBootstrapInfo := &github.GitHubInfo{
		RepoName:       bootstrapInfos.GitHubInfo.RepoName,
		RepoOwner:      bootstrapInfos.GitHubInfo.RepoOwner,
		BaseDomain:     bootstrapInfos.GitHubInfo.BaseDomain,
		LoadBalancerIP: bootstrapInfos.GitHubInfo.LoadBalancerIP,
	}

	_, err := githubClient.InitRepo(githubBootstrapInfo, false)
	if err != nil {
		m.Logger.Errorf("github init repo failed: %v", err)
	}

	// Create GitHub config for backend
	githubConfig = github.NewConfig(githubToken, bootstrapInfos.GitHubInfo.RepoOwner, bootstrapInfos.GitHubInfo.RepoName)

	m.Logger.Successf("Repo initialized! : https://github.com/%s/%s.git", bootstrapInfos.GitHubInfo.RepoOwner, bootstrapInfos.GitHubInfo.RepoName)
	s.IsDone = true
	return nil
}

func RunGitHubAuthStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {
	RunOAuthServerInBackround(m.Logger)
	go func() {
		var err error
		githubToken, err = oauthServer.Authenticate(context.Background(), "GitHub")
		if err != nil {
			m.Logger.Errorf("skipping, oauth server authenticate failed: %v", err)
			s.IsDone = true
			return
		}
		s.IsDone = true
	}()
	return nil
}

func RunWaitForServersStep(model *tui.Model, step *tui.Step) tea.Cmd {

	if doRestoreProgress {
		model.Logger.Info("State file found, skip looking for machines") // TODO ... FOR NOW!!
		step.IsDone = true
		step.AutoAdvance = true
		step.OnExit = nil
		return nil
	}

	model.Logger.Infof("Cluster: %s", bootstrapInfos.TalosInfo.ClusterName)
	addr := bootstrapInfos.TalosInfo.HTTPHostname + ":" + bootstrapInfos.TalosInfo.HTTPPort
	model.Logger.Infof("Starting HTTP Receive Server on %s …", addr)
	go func() {
		for i := 0; i < 5; i++ {
			err := talos.EventSink(bootstrapInfos, func(ctx context.Context, event events.Event) error {
				ip := strings.Split(event.Node, ":")[0]
				_, ok := saveState.MachinesDisks[ip]
				if !ok {
					saveState.MachinesDisks[ip] = ""
					step.Body = step.Body + fmt.Sprintf("\nNode: %s", ip)
					err := marshal.SaveStateToJSON(saveState)
					if err != nil {
						model.Logger.Errorf("Error saving state: %s", err)
					}
				}

				return nil
			})

			if err != nil {
				model.Logger.Errorf("Error with HTTP Receive Server, trying again...: %s", err)
			} else {
				model.Logger.Info("HTTP Receive Server stoped, restarting...")
			}

			time.Sleep(5 * time.Second)
		}
		model.Logger.Error("HTTP Server failed too many times")
	}()

	return nil
}

func ExitWaitForServersStep(model *tui.Model, step *tui.Step) {
	i := 0
	for k := range saveState.MachinesDisks {
		var disks []*storage.Disk
		// Insert start at the next step (after WaitForServer)
		model.Steps = slices.Insert(model.Steps, model.CurrentStepIndex+i+1, &tui.Step{
			Name:        fmt.Sprintf("ConfigureServer_%d", i),
			Title:       fmt.Sprintf("4.%d) Configure server %d", i, i),
			Kind:        tui.StepForm,
			IsDone:      true, // Set by server
			AutoAdvance: false,
			OnEnter:     RunConfigureServers(k, &disks),
			OnExit:      ExitConfigureServer(k, &disks),
			Fields:      tui.CreateFieldsForStruct[state.ServerConfig](),
		})
		i++
	}
}

func RunConfigureServers(serverIp string, disks *[]*storage.Disk) func(model *tui.Model, step *tui.Step) tea.Cmd {

	return func(model *tui.Model, step *tui.Step) tea.Cmd {
		var err error
		*disks, err = talos.GetDisks(context.Background(), serverIp)

		if err != nil {
			model.Logger.Errorf("Error getting disks: %s", err)
		}

		stringWriter := &strings.Builder{}

		stringWriter.WriteString(fmt.Sprintf("SERVER CONFIGURATION - %s:\n", serverIp))
		stringWriter.WriteString("\n\n")

		stringWriter.WriteString("Please select a role\n")
		tableRoles := tablewriter.NewWriter(stringWriter)
		tableRoles.SetHeader([]string{"Selection", "Role"})
		tableRoles.AppendBulk([][]string{
			{"1)", "control-plane"},
			{"2)", "worker"},
		})
		tableRoles.Render()
		step.Fields[0].Label = stringWriter.String()

		stringWriter.Reset()
		stringWriter.WriteString("Please select a disk\n")
		tableDisks := tablewriter.NewWriter(stringWriter)
		tableDisks.SetHeader([]string{"Selection", "Name", "Model", "UUID", "WWID", "Size"})
		for i, disk := range *disks {
			//jsonVal, _ := json.Marshal(disk)
			//model.Logger.Infof("Disk %d: %s", i, string(jsonVal))
			tableDisks.Append([]string{fmt.Sprintf("%d)", i+1), disk.DeviceName, disk.Model, disk.Uuid, disk.Wwid, strconv.FormatUint(disk.Size/_gigabyte, 10)})
		}
		tableDisks.Render()
		step.Fields[1].Label = stringWriter.String()

		return nil
	}
}

func ExitConfigureServer(serverIp string, disks *[]*storage.Disk) func(model *tui.Model, step *tui.Step) {
	return func(model *tui.Model, step *tui.Step) {

		config, err := tui.RetrieveStructFromFields[state.ServerConfig](step.Fields)
		if err != nil {
			model.Logger.Errorf("Error retrieving server config: %s", err)
		}

		if config.InstallDisk < 1 || config.InstallDisk > len(*disks) {
			model.Logger.Errorf("Invalid disk selection, skipping: %d", config.InstallDisk)
			return
		}

		defDisks := *disks
		saveState.MachinesDisks[serverIp] = defDisks[config.InstallDisk-1].BusPath

		switch config.Role {
		case 1:
			saveState.MachinesCache.ControlPlanes[serverIp] = make([]byte, 0)
			break
		case 2:
			saveState.MachinesCache.Workers[serverIp] = make([]byte, 0)
			break
		default:
			model.Logger.Errorf("Invalid role: %d", config.Role)
		}

		err = marshal.SaveStateToJSON(saveState)
		if err != nil {
			model.Logger.Errorf("Error saving state: %s", err)
		}
	}
}

func RunTalosISOStep(m *tui.Model, s *tui.Step) tea.Cmd {
	if doRestoreProgress {
		m.Logger.Info("State file found, skipping ISO download")
		s.IsDone = true
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		//talosConfigArg := fmt.Sprintf("talos.config=http://%s:%s/machineconfig?m=${mac}&u=${uuid}", bootstrapInfos.TalosInfo.HTTPHostname, bootstrapInfos.TalosInfo.HTTPPort)
		sinkConf := fmt.Sprintf("talos.events.sink=%s:%s", bootstrapInfos.TalosInfo.HTTPHostname, bootstrapInfos.TalosInfo.HTTPPort)
		kernelArgs := []string{sinkConf, bootstrapInfos.TalosInfo.TalosExtraArgs}

		m.Logger.Infof("Generating image with kernelParam: %s", sinkConf)

		factory := talos.CreateFactoryClient()
		sch := schematic.Schematic{
			Overlay: schematic.Overlay{
				Image: bootstrapInfos.TalosInfo.TalosOverlayImage,
				Name:  bootstrapInfos.TalosInfo.TalosOverlayName,
				// Options: nil, // ==> Extra YAML settings passed to overlay image.
			},
			Customization: schematic.Customization{
				ExtraKernelArgs: kernelArgs,
			},
		}

		schematicId, _ := factory.SchematicCreate(ctx, sch)
		m.Logger.Infof("Generated schematicId: %s", schematicId)

		talosImageFormat := "iso"
		if bootstrapInfos.TalosInfo.TalosArchitecture == "arm64" {
			talosImageFormat = "raw.xz"
		}

		talosImagePath := fmt.Sprintf("metal-%s.%s", bootstrapInfos.TalosInfo.TalosArchitecture, talosImageFormat)
		talosImageUrl := fmt.Sprintf("https://factory.talos.dev/image/%s/%s/%s", schematicId, bootstrapInfos.TalosInfo.TalosVersion, talosImagePath)
		//m.Logger.Infof("%s", talosImageUrl)

		m.Logger.Infof("Downloading image from %s...", talosImageUrl)
		resp, err := grab.Get(".", talosImageUrl)
		if err != nil {
			panic(fmt.Sprintf("Failed to download (%s) image! %s", talosImageUrl, err))
		}

		m.Logger.Successf("Download saved to: %s", resp.Filename)

		s.IsDone = true
	}()
	return nil
}

func RunClusterBootstrapStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {
	go func() {
		m.Logger.Debug("RunClusterBootstrapStepInBackground")

		m.Logger.Debug("Applying configs...")
		err := talos.ApplyConfigsToNodes(&saveState, bootstrapInfos)
		if err != nil {
			m.Logger.Errorf("Failed to apply configs: %s", err)
		}
		m.Logger.Debug("Configs applied")
		endpoint := state.ConfigBundle.ControlPlaneCfg.Cluster().Endpoint() //get machineconfig cluster endpoint
		talosApiClient := talos.CreateMachineryClientFromTalosconfig(state.ConfigBundle.TalosConfig())
		m.Logger.Infof("Executing bootstrap with clustername %s and endpoint %s....", bootstrapInfos.TalosInfo.ClusterName, endpoint)
		err = talos.ExecuteBootstrap(talosApiClient)
		if err != nil {
			m.Logger.Errorf("Failed to execute bootstrap: %s", err)
		}
		m.Logger.Success("Bootstrap request Succeeded!")
		m.Logger.Info("Waiting for Kubernetes installation to finish and API to be available...")

		//RunDetailedClusterHealthCheck(talosApiClient, m.Logger)
		time.Sleep(10 * time.Second)
		talos.RunBasicClusterHealthCheck(talosApiClient, m.Logger)
		m.Logger.Success("Cluster health check succeeded!")

		kubeconfig, err = talosApiClient.Kubeconfig(context.Background())
		if err != nil {
			m.Logger.Errorf("Failed to get kubeconfig: %v", err)
			panic(err)
		}

		err = os.WriteFile("kubeconfig", kubeconfig, 0600)

		if err != nil {
			m.Logger.Errorf("Failed to write kubeconfig: %v", err)
			panic(err)
		}

		m.Logger.Successf("Wrote kubeconfig to ./kubeconfig")
		m.Logger.Success("Your cluster is ready! You may now use kubectl to interact with the cluster")

		s.IsDone = true
	}()
	return nil
}

func RunArgoStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {
	go func() {
		m.Logger.Debug("RunArgoStepInBackground")
		DeployArgoCD(m.Logger)
		s.IsDone = true
	}()
	return nil
}

func RunPortalStepInBackground(m *tui.Model, s *tui.Step) tea.Cmd {
	go func() {
		m.Logger.Debug("RunPortalStepInBackground")
		CreateProviderSecrets(m.Logger)
		s.IsDone = true
	}()
	return nil
}

func DeployArgoCD(loggerRef *tui.UILogger) {
	loggerRef.Info("Setting up helm...")
	helmClient, err := helm.SetupHelmClient(loggerRef, kubeconfig)
	if err != nil {
		loggerRef.Errorf("Failed to setup helm client: %s", err)
		return
	}

	loggerRef.Infof("Deploying ArgoCD...")
	release, err := helm.HelmInstallArgo(helmClient)
	if err != nil {
		loggerRef.Errorf("Failed to deploy ArgoCD: %s", err)
		return
	}
	loggerRef.Successf("Successfully Installed release %s in namespace %s ; Notes:%s\n", release.Name, release.Namespace, release.Info.Notes)
}

func CreateProviderSecrets(loggerRef *tui.UILogger) {
	// Apply provider secrets
	k8sClient, err := k8s.NewClientFromKubeconfig(kubeconfig)
	if err != nil {
		loggerRef.Errorf("Failed to create Kubernetes client: %s", err)
	} else {
		ctx := context.Background()

		// Create namespace if it does not exists
		_, err = k8sClient.CoreV1().Namespaces().Get(ctx, "stolos-system", metav1.GetOptions{})
		if err != nil {
			_, err = k8sClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "stolos-system",
				},
			}, metav1.CreateOptions{})
		}

		// Create GCP service account secret
		if gcpConfig != nil {
			loggerRef.Info("Creating GCP service account secret...")
			err = gcpConfig.CreateOrUpdateSecret(ctx, k8sClient, "stolos-system", "gcp-service-account")
			if err != nil {
				loggerRef.Errorf("Failed to create GCP secret: %s", err)
			} else {
				loggerRef.Success("GCP service account secret created successfully")
			}
		}

		// Create GitHub credentials secret
		if githubConfig != nil {
			loggerRef.Info("Creating GitHub credentials secret...")
			err = githubConfig.CreateOrUpdateSecret(ctx, k8sClient, "stolos-system", "github-credentials")
			if err != nil {
				loggerRef.Errorf("Failed to create GitHub secret: %s", err)
			} else {
				loggerRef.Success("GitHub credentials secret created successfully")
			}
		}
	}
}

// Utils

// GetOutboundIP Get preferred outbound ip of this machine
// Ref: https://stackoverflow.com/a/37382208
func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}
