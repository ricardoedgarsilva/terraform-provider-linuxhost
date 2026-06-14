package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &fileResource{}

type fileResource struct {
	ssh *sshClient
}

type fileModel struct {
	Path    types.String `tfsdk:"path"`
	Content types.String `tfsdk:"content"`
	Mode    types.String `tfsdk:"mode"`
}

func newFileResource() resource.Resource {
	return &fileResource{}
}

func (r *fileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_file"
}

func (r *fileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"path": schema.StringAttribute{
				Required:    true,
				Description: "Absolute path on the remote host.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"content": schema.StringAttribute{
				Required:    true,
				WriteOnly:   true,
				Description: "File content. Write-only: never stored in state.",
			},
			"mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0600"),
			},
		},
	}
}

func (r *fileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *fileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data fileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes are null in Plan — read content from Config instead.
	var cfg fileModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Content = cfg.Content

	if err := r.write(data); err != nil {
		resp.Diagnostics.AddError("Failed to write file", err.Error())
		return
	}

	// Clear content before persisting — WriteOnly values must never enter state.
	data.Content = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read always removes the resource from state, making every apply re-write the file.
// This is intentional for secrets: they may rotate outside Terraform, so we always
// assert the desired content rather than tracking drift.
func (r *fileResource) Read(_ context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	resp.State.RemoveResource(context.Background())
}

func (r *fileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data fileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes are null in Plan — read content from Config instead.
	var cfg fileModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Content = cfg.Content

	if err := r.write(data); err != nil {
		resp.Diagnostics.AddError("Failed to write file", err.Error())
		return
	}

	data.Content = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *fileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data fileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.ssh.run(fmt.Sprintf("rm -f %q", data.Path.ValueString())); err != nil {
		resp.Diagnostics.AddError("Failed to remove file", err.Error())
	}
}

func (r *fileResource) write(data fileModel) error {
	cmd := fmt.Sprintf(
		"printf '%%s' %s | sudo tee %s > /dev/null && sudo chmod %s %s",
		shellQuote(data.Content.ValueString()),
		shellQuote(data.Path.ValueString()),
		data.Mode.ValueString(),
		shellQuote(data.Path.ValueString()),
	)
	_, err := r.ssh.exec(cmd)
	return err
}
