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
	"testing"

	"github.com/MAX-API-Next/MAX-API/types"
)

func TestSourceSelectionAndNoImplicitLegacyMigration(t *testing.T) {
	p := types.TaskPricingMaps{
		BillingMode:       map[string]string{"alias": "tiered_expr", "mapped": "tiered_expr", "*": "tiered_expr"},
		BillingExpr:       map[string]string{"alias": "1", "mapped": "2", "*": "99"},
		PluginBillingExpr: map[string]string{"kling::alias": "0", "kling::mapped": "3", "other::alias": "8"},
	}
	source := Source{Driver: "task_plugin", PluginKey: "kling"}
	check := func(expr, model, plugin string) {
		t.Helper()
		got, err := SelectExpression(p, source, "alias", "mapped")
		if err != nil || got != (Selection{Expression: expr, Model: model, PluginKey: plugin}) {
			t.Fatal(got, err)
		}
	}
	check("0", "alias", "kling")
	delete(p.PluginBillingExpr, "kling::alias")
	check("3", "mapped", "kling")
	delete(p.PluginBillingExpr, "kling::mapped")
	check("1", "alias", "")
	p.PluginBillingExpr["kling::alias"] = " "
	if _, err := SelectExpression(p, source, "alias", "mapped"); err == nil {
		t.Fatal("explicit invalid override fell through")
	}
	delete(p.PluginBillingExpr, "kling::alias")
	delete(p.BillingExpr, "alias")
	if _, err := SelectExpression(p, source, "alias", "mapped"); err == nil {
		t.Fatal("missing selected default fell through")
	}
	delete(p.BillingMode, "alias")
	check("2", "mapped", "")
	delete(p.BillingMode, "mapped")
	if _, err := SelectExpression(p, source, "alias", "mapped"); err == nil {
		t.Fatal("wildcard/other plugin leaked")
	}
	for _, s := range []Source{{"go_adapter", "kling"}, {"task_plugin", ""}, {"task_plugin", "kling::other"}} {
		if _, err := SelectExpression(p, s, "alias", "mapped"); err == nil {
			t.Fatal("invalid source accepted", s)
		}
	}
	p.BillingMode["alias"] = "tiered_expr"
	p.BillingExpr["alias"] = "4"
	p.PluginBillingExpr["kling::alias"] = "0"
	got, err := SelectExpression(p, Source{Driver: "go_adapter"}, "alias", "mapped")
	if err != nil || got.Expression != "4" || got.PluginKey != "" {
		t.Fatal("Go used phantom plugin", got, err)
	}
	for _, legacy := range []string{LegacyRateCard, LegacyMiniMax, LegacyToken} {
		profile, err := SelectProfile("", legacy, true)
		if err != nil || profile != legacy {
			t.Fatal("implicit legacy migration", err)
		}
		if _, err := SelectProfile(NewAPITask, legacy, false); err == nil {
			t.Fatal("unsupported new driver enabled")
		}
		profile, err = SelectProfile(NewAPITask, legacy, true)
		if err != nil || profile != NewAPITask {
			t.Fatal(err)
		}
	}
}

func TestIndependentContextGuards(t *testing.T) {
	current := Guards{PriceVersion: "same-price", ContractDigest: "unit-second", BindingVersion: "channel-1"}
	if err := CheckGuards(current, current); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []Guards{
		{"old-price", current.ContractDigest, current.BindingVersion},
		{current.PriceVersion, "unit-credit", current.BindingVersion},
		{current.PriceVersion, current.ContractDigest, "channel-2"},
		{"", current.ContractDigest, current.BindingVersion},
	} {
		if CheckGuards(stale, current) == nil {
			t.Fatal("stale context accepted", stale)
		}
	}
}
