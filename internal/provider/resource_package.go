package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &packageResource{}

type packageResource struct {
	ssh *sshClient
}

type packageModel struct {
	Name    types.String `tfsdk:"name"`
	Version types.String `tfsdk:"version"`
}

func newPackageResource() resource.Resource {
	return &packageResource{}
}

func (r *packageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_package"
}

func (r *packageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"version": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (r *packageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	sc, ok := req.ProviderData.(*sshClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.ssh = sc
}

func (r *packageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data packageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	out, err := r.ssh.run(fmt.Sprintf("flock /tmp/homelab-apt.lock env DEBIAN_FRONTEND=noninteractive apt-get install -y %q", name))
	if err != nil {
		resp.Diagnostics.AddError("Failed to install package", strings.TrimSpace(out))
		return
	}

	version, err := r.installedVersion(name)
	if err != nil {
		resp.Diagnostics.AddError("Package not found after install", err.Error())
		return
	}
	data.Version = types.StringValue(version)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *packageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data packageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version, err := r.installedVersion(data.Name.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}
	data.Version = types.StringValue(version)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is never called: name is ForceNew and version is Computed.
func (r *packageResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

func (r *packageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data packageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	out, err := r.ssh.run(fmt.Sprintf("flock /tmp/homelab-apt.lock env DEBIAN_FRONTEND=noninteractive apt-get remove -y %q", name))
	if err != nil {
		resp.Diagnostics.AddError("Failed to remove package", strings.TrimSpace(out))
	}
}

func (r *packageResource) installedVersion(name string) (string, error) {
	out, err := r.ssh.run(fmt.Sprintf("dpkg-query -W -f='${Version}' %q", name))
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("package %q is not installed", name)
	}
	return strings.TrimSpace(out), nil
}
