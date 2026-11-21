package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &directoryResource{}

type directoryResource struct {
	ssh *sshClient
}

type directoryModel struct {
	Path  types.String `tfsdk:"path"`
	Mode  types.String `tfsdk:"mode"`
	Owner types.String `tfsdk:"owner"`
	Group types.String `tfsdk:"group"`
}

type statResult struct {
	exists bool
	mode   string
	owner  string
	group  string
}

func newDirectoryResource() resource.Resource {
	return &directoryResource{}
}

func (r *directoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_directory"
}

func (r *directoryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"path": schema.StringAttribute{
				Required:    true,
				Description: "Absolute path on the remote host.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("0755"),
				Description: "Octal permission string, e.g. \"0755\".",
			},
			"owner": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Numeric UID or username.",
			},
			"group": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Numeric GID or group name.",
			},
		},
	}
}

func (r *directoryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *directoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data directoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	path := data.Path.ValueString()
	mode := data.Mode.ValueString()

	if _, err := r.ssh.run(fmt.Sprintf("mkdir -p %q", path)); err != nil {
		resp.Diagnostics.AddError("Failed to create directory", err.Error())
		return
	}
	if _, err := r.ssh.run(fmt.Sprintf("chmod %s %q", mode, path)); err != nil {
		resp.Diagnostics.AddError("Failed to chmod directory", err.Error())
		return
	}

	if !data.Owner.IsNull() && !data.Owner.IsUnknown() {
		owner, group := data.Owner.ValueString(), data.Group.ValueString()
		if group == "" {
			group = owner
		}
		if _, err := r.ssh.run(fmt.Sprintf("chown %s:%s %q", owner, group, path)); err != nil {
			resp.Diagnostics.AddError("Failed to set ownership", err.Error())
			return
		}
	}

	st, err := r.stat(path)
	if err != nil || !st.exists {
		resp.Diagnostics.AddError("Directory missing after create", fmt.Sprintf("stat: %v", err))
		return
	}
	data.Mode = types.StringValue(st.mode)
	data.Owner = types.StringValue(st.owner)
	data.Group = types.StringValue(st.group)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *directoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data directoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	st, err := r.stat(data.Path.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to stat directory", err.Error())
		return
	}
	if !st.exists {
		resp.State.RemoveResource(ctx)
		return
	}

	data.Mode = types.StringValue(st.mode)
	data.Owner = types.StringValue(st.owner)
	data.Group = types.StringValue(st.group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *directoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data directoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	path := data.Path.ValueString()

	if !data.Mode.IsNull() {
		if _, err := r.ssh.run(fmt.Sprintf("chmod %s %q", data.Mode.ValueString(), path)); err != nil {
			resp.Diagnostics.AddError("Failed to chmod directory", err.Error())
			return
		}
	}

	if !data.Owner.IsNull() {
		owner, group := data.Owner.ValueString(), data.Group.ValueString()
		if group == "" {
			group = owner
		}
		if _, err := r.ssh.run(fmt.Sprintf("chown %s:%s %q", owner, group, path)); err != nil {
			resp.Diagnostics.AddError("Failed to chown directory", err.Error())
			return
		}
	}

	st, err := r.stat(path)
	if err != nil || !st.exists {
		resp.Diagnostics.AddError("Directory missing after update", fmt.Sprintf("stat: %v", err))
		return
	}
	data.Mode = types.StringValue(st.mode)
	data.Owner = types.StringValue(st.owner)
	data.Group = types.StringValue(st.group)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete is intentionally a no-op — directories may contain service data.
// Removing a resource from config removes it from state only, not from disk.
func (r *directoryResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// stat queries the remote host for the directory's current permissions and ownership.
// Returns exists=false (no error) when the path is absent.
func (r *directoryResource) stat(path string) (statResult, error) {
	out, err := r.ssh.run(fmt.Sprintf(`stat -c '%%a %%u %%g' %q`, path))
	if err != nil {
		if strings.Contains(out, "No such file or directory") {
			return statResult{exists: false}, nil
		}
		return statResult{}, fmt.Errorf("stat: %s", strings.TrimSpace(out))
	}

	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 3 {
		return statResult{}, fmt.Errorf("unexpected stat output: %q", out)
	}

	return statResult{
		exists: true,
		mode:   "0" + parts[0],
		owner:  parts[1],
		group:  parts[2],
	}, nil
}
