package provider

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &syncResource{}
var _ resource.ResourceWithModifyPlan = &syncResource{}

type syncResource struct {
	ssh *sshClient
}

type syncModel struct {
	Source     types.String `tfsdk:"source"`
	Dest       types.String `tfsdk:"dest"`
	PostSync   types.String `tfsdk:"post_sync"`
	SourceHash types.String `tfsdk:"source_hash"`
}

func newSyncResource() resource.Resource {
	return &syncResource{}
}

func (r *syncResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sync"
}

func (r *syncResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"source": schema.StringAttribute{
				Required:    true,
				Description: "Local directory to mirror to the remote host.",
			},
			"dest": schema.StringAttribute{
				Required:    true,
				Description: "Absolute path on the remote host.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"post_sync": schema.StringAttribute{
				Optional:    true,
				Description: "Shell command to run on the remote host after a successful sync.",
			},
			"source_hash": schema.StringAttribute{
				Computed:    true,
				Description: "SHA256 hash of the source directory contents. Changes trigger a re-sync.",
			},
		},
	}
}

// ModifyPlan computes source_hash from the local directory during plan so Terraform
// can detect when files have changed and show an update before apply.
func (r *syncResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}

	var plan syncModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hash, err := hashDir(plan.Source.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to hash source directory", err.Error())
		return
	}

	plan.SourceHash = types.StringValue(hash)
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

func (r *syncResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *syncResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data syncModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.transfer(data.Source.ValueString(), data.Dest.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to sync directory", err.Error())
		return
	}

	if !data.PostSync.IsNull() && !data.PostSync.IsUnknown() {
		if err := r.postSync(data.PostSync.ValueString()); err != nil {
			resp.Diagnostics.AddError("post_sync failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *syncResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data syncModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.ssh.run(fmt.Sprintf("test -d %q", data.Dest.ValueString()))
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *syncResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data syncModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.transfer(data.Source.ValueString(), data.Dest.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to sync directory", err.Error())
		return
	}

	if !data.PostSync.IsNull() && !data.PostSync.IsUnknown() {
		if err := r.postSync(data.PostSync.ValueString()); err != nil {
			resp.Diagnostics.AddError("post_sync failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete is a no-op — compose directories are not removed on destroy.
func (r *syncResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// postSync runs the post_sync command, wrapping in sudo bash -c when sudo is enabled
// so that shell constructs (cd, &&, etc.) work correctly under elevated privileges.
func (r *syncResource) postSync(cmd string) error {
	if r.ssh.sudo {
		cmd = "sudo bash -c " + shellQuote(cmd)
	}
	out, err := r.ssh.exec(cmd)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(out))
	}
	return nil
}

// transfer tars the local source directory and extracts it on the remote dest via SSH stdin.
func (r *syncResource) transfer(source, dest string) error {
	sess, release, err := r.ssh.newSession()
	if err != nil {
		return err
	}
	defer release()
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}

	var errBuf strings.Builder
	sess.Stderr = &errBuf

	cmd := fmt.Sprintf("sudo mkdir -p %q && sudo tar --no-same-owner -x -C %q", dest, dest)
	if err := sess.Start(cmd); err != nil {
		return err
	}

	tw := tar.NewWriter(stdin)
	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		hdr := &tar.Header{
			Name: rel,
			Mode: int64(info.Mode()),
		}

		if info.IsDir() {
			hdr.Typeflag = tar.TypeDir
			return tw.WriteHeader(hdr)
		}

		hdr.Size = info.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})

	tw.Close()
	stdin.Close()

	if waitErr := sess.Wait(); waitErr != nil {
		return fmt.Errorf("remote extract failed: %s", strings.TrimSpace(errBuf.String()))
	}
	return walkErr
}

// hashDir computes a SHA256 over all file paths and contents in a directory.
func hashDir(path string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(path, p)
		h.Write([]byte(rel + "\n"))
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
