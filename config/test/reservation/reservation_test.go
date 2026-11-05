// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: CC0-1.0

package reservation

import (
	"context"
	"testing"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	clustercompute "github.com/upbound/provider-gcp/v3/config/cluster/compute"
	namespacedcompute "github.com/upbound/provider-gcp/v3/config/namespaced/compute"
)

// TestReservationCreatePreservesSharing checks the data delivered to the SDK
// create callback. The control uses the unmodified SDK diff; the regression
// cases pass that same diff through the real reservation configurator first.
// No credentials or GCP API calls are used.
func TestReservationCreatePreservesSharing(t *testing.T) {
	for scope, configure := range map[string]func(*config.Provider){
		"cluster":    clustercompute.Configure,
		"namespaced": namespacedcompute.Configure,
	} {
		t.Run(scope, func(t *testing.T) {
			for _, useCustomDiff := range []bool{false, true} {
				name := "sdk_control"
				if useCustomDiff {
					name = "provider_custom_diff"
				}
				t.Run(name, func(t *testing.T) {
					r := &schema.Resource{
						Schema: map[string]*schema.Schema{
							"zone": {Type: schema.TypeString, Optional: true},
							"share_settings": {
								Type: schema.TypeList, Optional: true, Computed: true, MaxItems: 1,
								Elem: &schema.Resource{Schema: map[string]*schema.Schema{
									"share_type": {Type: schema.TypeString, Optional: true, Computed: true, ForceNew: true},
									"project_map": {
										Type: schema.TypeSet, Optional: true,
										Elem: &schema.Resource{Schema: map[string]*schema.Schema{
											"id":         {Type: schema.TypeString, Required: true},
											"project_id": {Type: schema.TypeString, Optional: true},
										}},
									},
								}},
							},
						},
					}
					var received []interface{}
					r.Create = func(d *schema.ResourceData, _ interface{}) error {
						received = d.Get("share_settings").([]interface{})
						d.SetId("mock-reservation")
						return nil
					}

					// Register only the reservation fixture, then use the real
					// package configurator rather than copying its diff hook.
					p := config.NewProvider(
						[]byte(`{"format_version":"0.2","provider_schemas":{"registry.terraform.io/hashicorp/google":{"resource_schemas":{}}}}`),
						"google", "github.com/upbound/provider-gcp/v3", []byte("name: gcp\nresources: {}\n"),
					)
					p.Resources["google_compute_reservation"] = &config.Resource{TerraformResource: r}
					configure(p)
					p.ConfigureResources()

					state := &terraform.InstanceState{Attributes: map[string]string{}}
					cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
						"zone": "us-central1-a",
						"share_settings": []interface{}{map[string]interface{}{
							"share_type": "SPECIFIC_PROJECTS",
							"project_map": []interface{}{map[string]interface{}{
								"id": "100000000001", "project_id": "100000000001",
							}},
						}},
					})
					diff, err := schema.InternalMap(r.Schema).Diff(context.Background(), state, cfg, nil, nil, false)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("SDK sharing block count: %+v", diff.Attributes["share_settings.#"])
					if hook := p.Resources["google_compute_reservation"].TerraformCustomDiff; useCustomDiff && hook != nil {
						diff, err = hook(diff, state, cfg)
						if err != nil {
							t.Fatal(err)
						}
					}
					t.Logf("sharing block count passed to Apply: %+v", diff.Attributes["share_settings.#"])
					_, diagnostics := r.Apply(context.Background(), state, diff, nil)
					if diagnostics.HasError() {
						t.Fatal(diagnostics)
					}
					if len(received) != 1 {
						t.Fatalf("create callback received %d sharing blocks, want 1 with SPECIFIC_PROJECTS and consumer 100000000001", len(received))
					}
					sharing := received[0].(map[string]interface{})
					if got := sharing["share_type"]; got != "SPECIFIC_PROJECTS" {
						t.Errorf("create callback received share_type %v, want SPECIFIC_PROJECTS", got)
					}
					projects := sharing["project_map"].(*schema.Set).List()
					if len(projects) != 1 {
						t.Fatalf("create callback received %d consumer projects, want 1", len(projects))
					}
					if got := projects[0].(map[string]interface{})["project_id"]; got != "100000000001" {
						t.Errorf("create callback received consumer %v, want 100000000001", got)
					}
				})
			}
		})
	}
}
