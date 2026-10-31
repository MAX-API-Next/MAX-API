/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskpricing

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/types"
)

func previewInput(t *testing.T, f sample) PreviewInput {
	t.Helper()
	digest, err := ContractDigest(f.Contract)
	if err != nil {
		t.Fatal(err)
	}
	model := f.Models[0]
	guards := Guards{PriceVersion: "fixture-price-v1", ContractDigest: digest, BindingVersion: "fixture-binding-v1"}
	return PreviewInput{Profile: NewAPITask, Source: Source{Driver: "task_plugin", PluginKey: f.PluginKey}, OriginalModel: model,
		Prices:   types.TaskPricingMaps{PluginBillingExpr: map[string]string{f.PluginKey + "::" + model: f.Expression}},
		Contract: f.Contract, Expected: guards, Current: guards, Submit: f.Submit, Actual: f.Actual,
		QuotaPerUnit: float64(f.Expected.QuotaPerUnit), GroupRatio: float64(f.Expected.GroupRatio), ReservationFloor: f.Expected.Floor,
		PricingAt: time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)}
}

func TestTaskPreviewIndependentThreeFamilyAmounts(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.ID, func(t *testing.T) {
			in := previewInput(t, f)
			before, err := common.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			view, err := Preview(in)
			if err != nil {
				t.Fatal(err)
			}
			if view.Profile != NewAPITask || view.Source != in.Source || view.ContractDigest != in.Expected.ContractDigest {
				t.Fatal("preview identity lost", view)
			}
			if view.ReservedQuota != f.Expected.Reserved || view.Final == nil || view.Final.QuotaAfterGroup != f.Expected.Target || view.FinalMinusReserved == nil || -*view.FinalMinusReserved != f.Expected.Refund {
				t.Fatal("independent reservation/target/delta mismatch", view)
			}
			if math.Abs(view.Estimate.Amount-f.Expected.SubmitUSD) > 1e-10 || math.Abs(view.Final.Amount-f.Expected.FinalUSD) > 1e-10 {
				t.Fatal("currency mismatch", view)
			}
			if view.Final.PricingAt != view.Estimate.PricingAt {
				t.Fatal("preview clock changed between phases")
			}
			after, err := common.Marshal(in)
			if err != nil || string(before) != string(after) {
				t.Fatal("preview mutated host input", err)
			}
		})
	}
}

func TestTaskPreviewPhasePresenceAndZero(t *testing.T) {
	f := fixtures(t)[1]
	in := previewInput(t, f)
	in.Actual = nil
	view, err := Preview(in)
	if err != nil || view.Final != nil || view.FinalMinusReserved != nil {
		t.Fatal("missing final became zero target", view, err)
	}
	in.Actual = types.TaskPricingFacts{}
	if _, err := Preview(in); err == nil {
		t.Fatal("empty actual inherited submit estimate")
	}
	in.Actual = types.TaskPricingFacts{"units": float64(0)}
	in.ReservationFloor = 1200
	view, err = Preview(in)
	if err != nil || view.Final == nil || view.Final.QuotaAfterGroup != 0 || view.ReservedQuota != 1200 || view.FinalMinusReserved == nil || *view.FinalMinusReserved != -1200 {
		t.Fatal("zero or submit-only floor lost", view, err)
	}
	in.ReservationFloor = 0
	in.Actual = types.TaskPricingFacts{"units": float64(2)}
	view, err = Preview(in)
	if err != nil || view.FinalMinusReserved == nil || *view.FinalMinusReserved != 100 {
		t.Fatal("additional funding gap hidden", view, err)
	}
	// A selected explicit zero price remains zero even with an expensive default.
	in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = "0"
	in.Prices.BillingMode = map[string]string{in.OriginalModel: "tiered_expr"}
	in.Prices.BillingExpr = map[string]string{in.OriginalModel: "1000"}
	view, err = Preview(in)
	if err != nil || view.Estimate.Amount != 0 || view.Final.Amount != 0 || view.Selection.PluginKey != f.PluginKey {
		t.Fatal("explicit zero override fell through", view, err)
	}
}

func TestTaskPreviewStaticSchemaAndContextGuards(t *testing.T) {
	f := fixtures(t)[1]
	for _, mutate := range []func(*PreviewInput){
		func(in *PreviewInput) { in.Profile = LegacyRateCard },
		func(in *PreviewInput) { in.Expected.PriceVersion = "old" },
		func(in *PreviewInput) { in.Expected.ContractDigest = "old" },
		func(in *PreviewInput) { in.Expected.BindingVersion = "old" },
		func(in *PreviewInput) { in.Current.ContractDigest = "not-this-schema"; in.Expected = in.Current },
		func(in *PreviewInput) { in.ReservationFloor = -1 },
		func(in *PreviewInput) { in.ReservationFloor = common.MaxQuota + 1 },
		func(in *PreviewInput) {
			in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = `false ? u("undeclared") : 0.1`
		},
		func(in *PreviewInput) {
			in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = `false ? u(param("fast")) : 0.1`
		},
		func(in *PreviewInput) {
			field := in.Contract.UsageSchema["units"]
			field.Unit = "token"
			in.Contract.UsageSchema["units"] = field
		},
	} {
		in := previewInput(t, fixtures(t)[1])
		mutate(&in)
		if _, err := Preview(in); err == nil {
			t.Fatal("stale/invalid preview accepted")
		}
	}
	// Optional schema facts are allowed to be absent when the expression does
	// not evaluate them; they must still be declared even in a hidden branch.
	in := previewInput(t, fixtures(t)[1])
	field := in.Contract.UsageSchema["units"]
	in.Contract.UsageSchema["optional"] = field
	in.Contract.FieldPolicy["optional"] = types.TaskFactPolicy{Phase: "actual"}
	digest, err := ContractDigest(in.Contract)
	if err != nil {
		t.Fatal(err)
	}
	in.Current.ContractDigest = digest
	in.Expected = in.Current
	in.Prices.PluginBillingExpr[f.PluginKey+"::"+in.OriginalModel] = `false ? u("optional") : u("units") * 0.1`
	view, err := Preview(in)
	if err != nil || !reflect.DeepEqual(view.Source, in.Source) {
		t.Fatal(view, err)
	}
}

func TestTaskPreviewRejectsUnfrozenLocalTimezone(t *testing.T) {
	for _, expression := range []string{
		`hour("Local") == 0 ? 1 : 2`,
		`false ? hour("Local") : 1`,
		`u("units") >= 1 ? 0.1 : hour(string(param("service_tier")))`,
		`now().Year()`,
		`date("2026-10-30T00:00:00Z").Local().Hour()`,
		`false ? (timezone("Local").String() == "Local" ? 1 : 2) : 1`,
	} {
		in := previewInput(t, fixtures(t)[1])
		in.Prices.PluginBillingExpr["kling::"+in.OriginalModel] = expression
		in.Request = billingexpr.RequestInput{Body: []byte(`{"service_tier":"Local"}`)}
		view, err := Preview(in)
		if err == nil {
			t.Fatalf("unfrozen time preview accepted (%s): %+v", expression, view)
		}
		if view.Profile != "" || view.Final != nil {
			t.Fatalf("invalid timezone returned a usable preview: %+v", view)
		}
	}
}
