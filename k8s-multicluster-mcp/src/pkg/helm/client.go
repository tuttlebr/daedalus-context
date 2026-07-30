package helm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps Helm operations
type Client struct {
	settings   *cli.EnvSettings
	restConfig *rest.Config
	k8sClient  kubernetes.Interface
}

// NewClient creates a new Helm client
func NewClient(kubeconfig string) (*Client, error) {
	settings := cli.New()

	if kubeconfig != "" {
		settings.KubeConfig = kubeconfig
	}

	// Get Kubernetes REST config
	var restConfig *rest.Config
	var err error

	if settings.KubeConfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", settings.KubeConfig)
	} else {
		if home := os.Getenv("HOME"); home != "" {
			defaultKubeconfig := filepath.Join(home, ".kube", "config")
			if _, statErr := os.Stat(defaultKubeconfig); statErr == nil {
				restConfig, err = clientcmd.BuildConfigFromFlags("", defaultKubeconfig)
			} else {
				restConfig, err = rest.InClusterConfig()
			}
		} else {
			restConfig, err = rest.InClusterConfig()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}
	if restConfig.Timeout == 0 {
		restConfig.Timeout = 30 * time.Second
	}

	// Create Kubernetes client
	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	c := &Client{
		settings:   settings,
		restConfig: restConfig,
		k8sClient:  k8sClient,
	}

	// Ensure the Helm repository config file exists so that repo operations
	// don't fail on a fresh container with no prior helm usage.
	if err := c.ensureRepoFile(); err != nil {
		return nil, fmt.Errorf("failed to initialize helm repository config: %w", err)
	}

	return c, nil
}

// ensureRepoFile creates the Helm repositories.yaml with an empty repo list
// if it does not already exist.
func (c *Client) ensureRepoFile() error {
	repoFile := c.settings.RepositoryConfig
	if err := os.MkdirAll(filepath.Dir(repoFile), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(repoFile); errors.Is(err, os.ErrNotExist) {
		f := repo.NewFile()
		return f.WriteFile(repoFile, 0644)
	}
	return nil
}

// initActionConfig creates and initializes a Helm action configuration for the given namespace.
func (c *Client) initActionConfig(namespace string) (*action.Configuration, error) {
	cfg := &action.Configuration{}
	if err := cfg.Init(c.settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
		return nil, fmt.Errorf("failed to initialize action config: %w", err)
	}

	// Create a registry client so that OCI chart references (oci://...) can be resolved.
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}
	cfg.RegistryClient = registryClient

	return cfg, nil
}

// InstallOptions holds all options for the InstallChart operation,
// corresponding to the flags accepted by `helm install`.
type InstallOptions struct {
	// Chart source / version
	Version               string
	Devel                 bool
	RepoURL               string
	Username              string
	Password              string
	CaFile                string
	CertFile              string
	KeyFile               string
	InsecureSkipTLSVerify bool
	PassCredentials       bool
	PlainHTTP             bool
	Verify                bool

	// Values
	ValuesFiles []string // paths to YAML values files (-f / --values)

	// Install identity
	CreateNamespace  bool
	GenerateName     bool
	NameTemplate     string
	Description      string
	Labels           map[string]string
	DependencyUpdate bool

	// Deployment behavior
	Wait        bool
	WaitForJobs bool
	Timeout     time.Duration
	Atomic      bool

	// Dry-run
	DryRunOption string // "client" or "server"; empty = disabled
	HideSecret   bool

	// Resource handling
	Force         bool
	Replace       bool
	SkipCRDs      bool
	DisableHooks  bool
	TakeOwnership bool

	// Validation
	SkipSchemaValidation     bool
	DisableOpenAPIValidation bool

	// Output / rendering
	SubNotes  bool // render subchart notes
	HideNotes bool
	EnableDNS bool
}

func (c *Client) InstallChart(ctx context.Context, namespace, releaseName, chartName string, opts InstallOptions, values map[string]interface{}) (*release.Release, error) {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	client := action.NewInstall(actionConfig)
	client.Namespace = namespace
	client.ReleaseName = releaseName

	// Apply all options to the action client
	client.CreateNamespace = opts.CreateNamespace
	client.Wait = opts.Wait
	client.WaitForJobs = opts.WaitForJobs
	if opts.Timeout > 0 {
		client.Timeout = opts.Timeout
	}
	client.Atomic = opts.Atomic
	client.Force = opts.Force
	client.Replace = opts.Replace
	client.SkipCRDs = opts.SkipCRDs
	client.DisableHooks = opts.DisableHooks
	client.TakeOwnership = opts.TakeOwnership
	client.GenerateName = opts.GenerateName
	client.NameTemplate = opts.NameTemplate
	client.Description = opts.Description
	client.Labels = opts.Labels
	client.DependencyUpdate = opts.DependencyUpdate
	client.Devel = opts.Devel
	client.SkipSchemaValidation = opts.SkipSchemaValidation
	client.DisableOpenAPIValidation = opts.DisableOpenAPIValidation
	client.SubNotes = opts.SubNotes
	client.HideNotes = opts.HideNotes
	client.HideSecret = opts.HideSecret
	client.EnableDNS = opts.EnableDNS
	if opts.DryRunOption != "" {
		client.DryRun = true
		client.DryRunOption = opts.DryRunOption
	}

	// Chart path / auth options
	if opts.RepoURL != "" {
		client.RepoURL = opts.RepoURL
	}
	client.Version = opts.Version
	client.Username = opts.Username
	client.Password = opts.Password
	client.CaFile = opts.CaFile
	client.CertFile = opts.CertFile
	client.KeyFile = opts.KeyFile
	client.InsecureSkipTLSverify = opts.InsecureSkipTLSVerify
	client.PassCredentialsAll = opts.PassCredentials
	client.PlainHTTP = opts.PlainHTTP
	client.Verify = opts.Verify

	if values == nil {
		values = make(map[string]interface{})
	}

	// Load values files (earlier files are lower priority; direct values override all)
	if len(opts.ValuesFiles) > 0 {
		merged := make(map[string]interface{})
		for _, f := range opts.ValuesFiles {
			fVals, err := chartutil.ReadValuesFile(f)
			if err != nil {
				return nil, fmt.Errorf("failed to read values file %q: %w", f, err)
			}
			mergeMaps(merged, fVals)
		}
		mergeMaps(merged, values)
		values = merged
	}

	// Locate the chart (resolves repo/chart or OCI)
	chartPath, err := client.LocateChart(chartName, c.settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}

	// Load the chart from the resolved path
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Run the install action
	rel, err := client.Run(chart, values)
	if err != nil {
		return nil, fmt.Errorf("failed to install chart: %w", err)
	}

	return rel, nil
}

// mergeMaps deep-merges src into dst; src values take precedence. Mutates dst.
func mergeMaps(dst, src map[string]interface{}) {
	for k, v := range src {
		if vMap, ok := v.(map[string]interface{}); ok {
			if dstMap, ok := dst[k].(map[string]interface{}); ok {
				mergeMaps(dstMap, vMap)
				continue
			}
		}
		dst[k] = v
	}
}

// UpgradeOptions holds all options for the UpgradeChart operation,
// corresponding to the flags accepted by `helm upgrade`.
type UpgradeOptions struct {
	// Chart source / version
	Version               string
	Devel                 bool
	RepoURL               string
	Username              string
	Password              string
	CaFile                string
	CertFile              string
	KeyFile               string
	InsecureSkipTLSVerify bool
	PassCredentials       bool
	PlainHTTP             bool
	Verify                bool

	// Values
	ValuesFiles []string // paths to YAML values files (-f / --values)

	// Release metadata
	Description string
	Labels      map[string]string

	// Deployment behavior
	Wait          bool
	WaitForJobs   bool
	Timeout       time.Duration
	Atomic        bool
	CleanupOnFail bool

	// Dry-run
	DryRunOption string // "client" or "server"; empty = disabled

	// Resource handling
	Force        bool
	DisableHooks bool
	SkipCRDs     bool

	// Values reuse strategy
	ReuseValues          bool
	ResetValues          bool
	ResetThenReuseValues bool

	// Validation
	DisableOpenAPIValidation bool

	// Output / rendering
	SubNotes  bool
	HideNotes bool
	EnableDNS bool

	// History cap (0 = unlimited)
	MaxHistory int
}

func (c *Client) UpgradeChart(ctx context.Context, namespace, releaseName, chartName string, opts UpgradeOptions, values map[string]interface{}) (*release.Release, error) {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	client := action.NewUpgrade(actionConfig)
	client.Namespace = namespace

	// Chart source / version
	client.Version = opts.Version
	client.Devel = opts.Devel
	if opts.RepoURL != "" {
		client.RepoURL = opts.RepoURL
	}
	client.Username = opts.Username
	client.Password = opts.Password
	client.CaFile = opts.CaFile
	client.CertFile = opts.CertFile
	client.KeyFile = opts.KeyFile
	client.InsecureSkipTLSverify = opts.InsecureSkipTLSVerify
	client.PassCredentialsAll = opts.PassCredentials
	client.PlainHTTP = opts.PlainHTTP
	client.Verify = opts.Verify

	// Release metadata
	client.Description = opts.Description
	client.Labels = opts.Labels

	// Deployment behavior
	client.Wait = opts.Wait
	client.WaitForJobs = opts.WaitForJobs
	if opts.Timeout > 0 {
		client.Timeout = opts.Timeout
	}
	client.Atomic = opts.Atomic
	client.CleanupOnFail = opts.CleanupOnFail

	// Dry-run
	if opts.DryRunOption != "" {
		client.DryRun = true
		client.DryRunOption = opts.DryRunOption
	}

	// Resource handling
	client.Force = opts.Force
	client.DisableHooks = opts.DisableHooks
	client.SkipCRDs = opts.SkipCRDs

	// Values reuse strategy
	client.ReuseValues = opts.ReuseValues
	client.ResetValues = opts.ResetValues
	client.ResetThenReuseValues = opts.ResetThenReuseValues

	// Validation / output
	client.DisableOpenAPIValidation = opts.DisableOpenAPIValidation
	client.SubNotes = opts.SubNotes
	client.HideNotes = opts.HideNotes
	client.EnableDNS = opts.EnableDNS

	if opts.MaxHistory > 0 {
		client.MaxHistory = opts.MaxHistory
	}

	if values == nil {
		values = make(map[string]interface{})
	}

	// Load values files (earlier files are lower priority; direct values override all)
	if len(opts.ValuesFiles) > 0 {
		merged := make(map[string]interface{})
		for _, f := range opts.ValuesFiles {
			fVals, err := chartutil.ReadValuesFile(f)
			if err != nil {
				return nil, fmt.Errorf("failed to read values file %q: %w", f, err)
			}
			mergeMaps(merged, fVals)
		}
		mergeMaps(merged, values)
		values = merged
	}

	// Locate the chart (for both OCI and regular charts)
	chartPath, err := client.LocateChart(chartName, c.settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	rel, err := client.Run(releaseName, chart, values)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade chart: %w", err)
	}

	return rel, nil
}

// UninstallChart uninstalls a Helm release
func (c *Client) UninstallChart(ctx context.Context, namespace, releaseName string) error {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return err
	}

	client := action.NewUninstall(actionConfig)
	_, err = client.Run(releaseName)
	if err != nil {
		return fmt.Errorf("failed to uninstall release: %w", err)
	}

	return nil
}

func (c *Client) ListReleases(ctx context.Context, namespace string) ([]*release.Release, error) {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	client := action.NewList(actionConfig)
	client.AllNamespaces = namespace == ""

	releases, err := client.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}
	// remove useless fields from releases
	for _, release := range releases {
		release.Chart.Templates = nil
		release.Chart.Files = nil
		release.Chart.Values = nil
		release.Chart.Schema = nil
		release.Config = nil
		release.Manifest = ""
		release.Chart.Lock = nil
		release.Hooks = nil
	}

	return releases, nil
}

func (c *Client) GetRelease(ctx context.Context, namespace, releaseName string) (*release.Release, error) {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	client := action.NewGet(actionConfig)
	release, err := client.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release: %w", err)
	}

	return release, nil
}

func (c *Client) GetReleaseHistory(ctx context.Context, namespace, releaseName string) ([]*release.Release, error) {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return nil, err
	}

	client := action.NewHistory(actionConfig)
	releases, err := client.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get release history: %w", err)
	}

	return releases, nil
}

// RollbackRelease rolls back a Helm release
func (c *Client) RollbackRelease(ctx context.Context, namespace, releaseName string, revision int) error {
	actionConfig, err := c.initActionConfig(namespace)
	if err != nil {
		return err
	}

	client := action.NewRollback(actionConfig)
	client.Version = revision

	if err := client.Run(releaseName); err != nil {
		return fmt.Errorf("failed to rollback release: %w", err)
	}

	return nil
}

// addRepo adds a Helm repository
func (c *Client) HelmRepoAdd(ctx context.Context, name, url string) error {
	repoFile := c.settings.RepositoryConfig

	// Ensure the file directory exists
	if err := os.MkdirAll(filepath.Dir(repoFile), 0755); err != nil {
		return err
	}

	// Load existing repositories
	f, err := repo.LoadFile(repoFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if f == nil {
		f = repo.NewFile()
	}

	// Check if repo already exists
	if f.Has(name) {
		return nil // Already exists
	}

	// Add the repository
	entry := &repo.Entry{
		Name: name,
		URL:  url,
	}

	r, err := repo.NewChartRepository(entry, getter.All(c.settings))
	if err != nil {
		return err
	}

	if _, err := r.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to download repository index: %w", err)
	}

	f.Update(entry)
	return f.WriteFile(repoFile, 0644)
}

func (c *Client) HelmRepoList(ctx context.Context) ([]*repo.Entry, error) {
	repoFile := c.settings.RepositoryConfig
	f, err := repo.LoadFile(repoFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load repository file: %w", err)
	}
	return f.Repositories, nil
}
