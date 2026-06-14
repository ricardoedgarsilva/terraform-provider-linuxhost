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

var _ resource.Resource = &cronResource{}

type cronResource struct {
	ssh *sshClient
}

type cronModel struct {
	Name    types.String `tfsdk:"name"`
	Minute  types.String `tfsdk:"minute"`
	Hour    types.String `tfsdk:"hour"`
	Day     types.String `tfsdk:"day"`
	Month   types.String `tfsdk:"month"`
	Weekday types.String `tfsdk:"weekday"`
	User    types.String `tfsdk:"user"`
	Job     types.String `tfsdk:"job"`
}

func newCronResource() resource.Resource {
	return &cronResource{}
}

func (r *cronResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cron"
}

func (r *cronResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique job name. Used as the filename in /etc/cron.d/.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"minute": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("*"),
			},
			"hour": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("*"),
			},
			"day": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("*"),
			},
			"month": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("*"),
			},
			"weekday": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("*"),
			},
			"user": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("root"),
			},
			"job": schema.StringAttribute{
				Required:    true,
				Description: "The command to execute.",
			},
		},
	}
}

func (r *cronResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *cronResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.write(data); err != nil {
		resp.Diagnostics.AddError("Failed to write cron job", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *cronResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parsed, err := r.read(data.Name.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, parsed)...)
}

func (r *cronResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data cronModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.write(data); err != nil {
		resp.Diagnostics.AddError("Failed to update cron job", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *cronResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data cronModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	path := cronPath(data.Name.ValueString())
	if _, err := r.ssh.run(fmt.Sprintf("rm -f %q", path)); err != nil {
		resp.Diagnostics.AddError("Failed to remove cron job", err.Error())
	}
}

func (r *cronResource) write(data cronModel) error {
	content := fmt.Sprintf(
		"# Managed by terraform-provider-linuxhost\n%s %s %s %s %s %s %s\n",
		data.Minute.ValueString(),
		data.Hour.ValueString(),
		data.Day.ValueString(),
		data.Month.ValueString(),
		data.Weekday.ValueString(),
		data.User.ValueString(),
		data.Job.ValueString(),
	)
	path := cronPath(data.Name.ValueString())
	// printf runs unprivileged; sudo tee writes to the protected /etc/cron.d/.
	// A plain redirect (> path) would execute as the SSH user, not root.
	cmd := fmt.Sprintf("printf '%%s' %s | sudo tee %s > /dev/null", shellQuote(content), shellQuote(path))
	_, err := r.ssh.exec(cmd)
	return err
}

func (r *cronResource) read(name string) (*cronModel, error) {
	out, err := r.ssh.run(fmt.Sprintf("cat %q", cronPath(name)))
	if err != nil {
		return nil, fmt.Errorf("cron job %q not found", name)
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 7 {
			return nil, fmt.Errorf("malformed cron line: %q", line)
		}
		return &cronModel{
			Name:    types.StringValue(name),
			Minute:  types.StringValue(parts[0]),
			Hour:    types.StringValue(parts[1]),
			Day:     types.StringValue(parts[2]),
			Month:   types.StringValue(parts[3]),
			Weekday: types.StringValue(parts[4]),
			User:    types.StringValue(parts[5]),
			Job:     types.StringValue(strings.Join(parts[6:], " ")),
		}, nil
	}
	return nil, fmt.Errorf("no cron entry found in %q", cronPath(name))
}

func cronPath(name string) string {
	return "/etc/cron.d/homelab-" + name
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
