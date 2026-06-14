package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/crypto/ssh"
)

var _ provider.Provider = &hostProvider{}

type hostProvider struct{}

type hostProviderModel struct {
	Host       types.String `tfsdk:"host"`
	User       types.String `tfsdk:"user"`
	PrivateKey types.String `tfsdk:"private_key"`
	Sudo       types.Bool   `tfsdk:"sudo"`
}

// sshClient is passed as provider data to every resource.
// newSession() is the single entry point for all SSH operations —
// it enforces the session semaphore and reconnects if the connection is dead.
type sshClient struct {
	mu     sync.Mutex
	client *ssh.Client
	sudo   bool
	host   string
	cfg    *ssh.ClientConfig
	// sem caps concurrent SSH sessions below the server's MaxSessions limit.
	sem chan struct{}
}

// run executes cmd on the remote host, prepending sudo when configured.
func (sc *sshClient) run(cmd string) (string, error) {
	if sc.sudo {
		cmd = "sudo " + cmd
	}
	return sc.exec(cmd)
}

// exec executes cmd verbatim (no sudo prefix). Used when the caller has
// already embedded privilege escalation in the command (printf | sudo tee, etc.).
func (sc *sshClient) exec(cmd string) (string, error) {
	backoffs := []time.Duration{0, 500 * time.Millisecond, time.Second}
	var lastErr error
	for i, wait := range backoffs {
		if i > 0 {
			time.Sleep(wait)
		}
		sess, release, err := sc.newSession()
		if err != nil {
			lastErr = err
			continue
		}
		out, err := sess.CombinedOutput(cmd)
		sess.Close()
		release()
		// Return immediately — the command ran; its result is definitive.
		return string(out), err
	}
	return "", lastErr
}

// newSession acquires a semaphore slot and opens an SSH session.
// If NewSession fails it attempts a reconnect before giving up.
// Callers must invoke the returned release func after the session is closed.
func (sc *sshClient) newSession() (*ssh.Session, func(), error) {
	sc.sem <- struct{}{}
	release := func() { <-sc.sem }

	// Snapshot current client outside the lock so concurrent callers can
	// proceed without serialising on session opens.
	sc.mu.Lock()
	current := sc.client
	sc.mu.Unlock()

	sess, err := current.NewSession()
	if err == nil {
		return sess, release, nil
	}

	// Session failed — reconnect under lock.
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Another goroutine may have already reconnected; use their client.
	if sc.client != current {
		sess, err = sc.client.NewSession()
		if err != nil {
			release()
			return nil, nil, err
		}
		return sess, release, nil
	}

	newClient, dialErr := dial(sc.host, sc.cfg)
	if dialErr != nil {
		release()
		return nil, nil, fmt.Errorf("SSH reconnect failed: %w", dialErr)
	}
	sc.client.Close()
	sc.client = newClient

	sess, err = newClient.NewSession()
	if err != nil {
		release()
		return nil, nil, err
	}
	return sess, release, nil
}

func New() func() provider.Provider {
	return func() provider.Provider {
		return &hostProvider{}
	}
}

func (p *hostProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "linuxhost"
	resp.Version = "0.1.0"
}

func (p *hostProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:    true,
				Description: "IP address or hostname of the Linux host (port 22).",
			},
			"user": schema.StringAttribute{
				Required:    true,
				Description: "SSH username.",
			},
			"private_key": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "PEM-encoded SSH private key.",
			},
			"sudo": schema.BoolAttribute{
				Optional:    true,
				Description: "Prefix all remote commands with sudo.",
			},
		},
	}
}

func (p *hostProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config hostProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	signer, err := ssh.ParsePrivateKey([]byte(config.PrivateKey.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Invalid private key", err.Error())
		return
	}

	cfg := &ssh.ClientConfig{
		User:            config.User.ValueString(),
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	}

	host := config.Host.ValueString()
	client, err := dial(host, cfg)
	if err != nil {
		resp.Diagnostics.AddError("SSH connection failed", err.Error())
		return
	}

	out, err := runOnce(client, "cat /etc/os-release")
	if err != nil {
		resp.Diagnostics.AddError("Failed to read /etc/os-release", err.Error())
		return
	}
	if !strings.Contains(out, "ID=ubuntu") {
		resp.Diagnostics.AddError(
			"Unsupported OS",
			fmt.Sprintf("This provider is still under development and requires Ubuntu. /etc/os-release reports:\n%s", out),
		)
		return
	}

	sc := &sshClient{
		client: client,
		sudo:   config.Sudo.ValueBool(),
		host:   host,
		cfg:    cfg,
		sem:    make(chan struct{}, 8),
	}
	resp.DataSourceData = sc
	resp.ResourceData = sc
}

func (p *hostProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newDirectoryResource,
		newPackageResource,
		newCronResource,
		newSyncResource,
		newFileResource,
	}
}

func (p *hostProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// dial opens an SSH connection, retrying up to 3 times with a 2 s gap.
func dial(host string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	var err error
	for attempt := range 3 {
		var c *ssh.Client
		c, err = ssh.Dial("tcp", host+":22", cfg)
		if err == nil {
			return c, nil
		}
		if attempt < 2 {
			time.Sleep(2 * time.Second)
		}
	}
	return nil, err
}

// runOnce opens a single session and runs cmd. Used during Configure before
// the sshClient is constructed.
func runOnce(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}
